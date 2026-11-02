package youtube

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

type resumeState struct {
	SessionURI string `json:"session_uri"`
}

var errSessionGone = errors.New("upload session no longer exists")

// sessionStatus is the state of a resumable session.
type sessionStatus struct {
	done   bool
	result provider.Result
	offset int64
}

// Upload runs the resumable upload protocol. The final chunk creates the video, so the job
// reports PhaseCommitting before sending it; afterwards only status queries are allowed.
func (p *Provider) Upload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (provider.Result, error) {
	var st resumeState
	if len(req.Resume) > 0 {
		json.Unmarshal(req.Resume, &st)
	}
	phase := req.Phase
	committing := provider.PhaseAtLeast(phase, provider.PhaseCommitting)
	var offset int64

	if st.SessionURI != "" {
		s, err := p.query(ctx, st.SessionURI, req)
		switch {
		case err == nil && s.done:
			return s.result, nil
		case err == nil:
			offset = s.offset
		case errors.Is(err, errSessionGone) && committing:
			return provider.Result{}, provider.OutcomeUnknown("the YouTube upload session expired after the final chunk was sent; check YouTube Studio for the video")
		case errors.Is(err, errSessionGone):
			st = resumeState{}
		default:
			return provider.Result{}, err
		}
	} else if committing {
		return provider.Result{}, provider.OutcomeUnknown("no upload session recorded for a committing job")
	}

	if st.SessionURI == "" {
		uri, err := p.startSession(ctx, req)
		if err != nil {
			return provider.Result{}, err
		}
		st, phase, offset = resumeState{SessionURI: uri}, provider.PhaseTransferring, 0
		if err := progress(provider.ProgressUpdate{BytesSent: 0, Phase: phase, Resume: mustJSON(st)}); err != nil {
			return provider.Result{}, err
		}
	}

	chunk := p.ChunkSize
	if chunk <= 0 || chunk%chunkMultiple != 0 {
		chunk = defaultChunk
	}
	for offset < req.FileSize {
		end := min(offset+chunk, req.FileSize)
		if end == req.FileSize && !provider.PhaseAtLeast(phase, provider.PhaseCommitting) {
			phase = provider.PhaseCommitting
			if err := progress(provider.ProgressUpdate{BytesSent: offset, Phase: phase, Resume: mustJSON(st)}); err != nil {
				return provider.Result{}, err
			}
		}
		s, err := p.putChunk(ctx, st.SessionURI, req, offset, end)
		if err != nil {
			if provider.PhaseAtLeast(phase, provider.PhaseCommitting) && ctx.Err() == nil && !provider.IsPermanent(err) {
				// The final chunk may have landed; ask the session before giving up.
				if qs, qerr := p.query(ctx, st.SessionURI, req); qerr == nil && qs.done {
					return qs.result, nil
				}
			}
			return provider.Result{}, err
		}
		if s.done {
			return s.result, nil
		}
		offset = s.offset
		progress(provider.ProgressUpdate{BytesSent: offset})
	}
	// All bytes acknowledged without a final response: ask once more.
	s, err := p.query(ctx, st.SessionURI, req)
	if err == nil && s.done {
		return s.result, nil
	}
	return provider.Result{}, provider.OutcomeUnknown("YouTube acknowledged every byte but returned no video")
}

func (p *Provider) startSession(ctx context.Context, req provider.UploadRequest) (string, error) {
	status := map[string]any{
		"privacyStatus":           req.Meta.Privacy,
		"selfDeclaredMadeForKids": provider.BoolOption(req.Meta.Options, "made_for_kids", false),
	}
	body := map[string]any{
		"snippet": map[string]any{
			"title":       req.Meta.Title,
			"description": req.Meta.Description,
			"tags":        req.Meta.Tags,
			"categoryId":  provider.StringOption(req.Meta.Options, "category_id", "22"),
		},
		"status": status,
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.UploadBase+"/videos?uploadType=resumable&part=snippet,status", bytes.NewReader(mustJSON(body)))
	if err != nil {
		return "", err
	}
	r.Header.Set("Authorization", "Bearer "+req.Token.AccessToken)
	r.Header.Set("Content-Type", "application/json; charset=UTF-8")
	r.Header.Set("X-Upload-Content-Length", strconv.FormatInt(req.FileSize, 10))
	r.Header.Set("X-Upload-Content-Type", provider.MIMEType(req.Container))
	resp, err := p.HTTP.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", apiError(resp, data)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", errors.New("YouTube did not return an upload session URL")
	}
	return loc, nil
}

func (p *Provider) putChunk(ctx context.Context, uri string, req provider.UploadRequest, start, end int64) (sessionStatus, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, uri, io.NewSectionReader(req.File, start, end-start))
	if err != nil {
		return sessionStatus{}, err
	}
	r.ContentLength = end - start
	r.Header.Set("Authorization", "Bearer "+req.Token.AccessToken)
	r.Header.Set("Content-Type", provider.MIMEType(req.Container))
	r.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, req.FileSize))
	return p.sessionResponse(r)
}

func (p *Provider) query(ctx context.Context, uri string, req provider.UploadRequest) (sessionStatus, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, uri, nil)
	if err != nil {
		return sessionStatus{}, err
	}
	r.ContentLength = 0
	r.Header.Set("Authorization", "Bearer "+req.Token.AccessToken)
	r.Header.Set("Content-Range", fmt.Sprintf("bytes */%d", req.FileSize))
	return p.sessionResponse(r)
}

func (p *Provider) sessionResponse(r *http.Request) (sessionStatus, error) {
	resp, err := p.HTTP.Do(r)
	if err != nil {
		return sessionStatus{}, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var v struct {
			ID     string `json:"id"`
			Status struct {
				PrivacyStatus string `json:"privacyStatus"`
			} `json:"status"`
		}
		if err := json.Unmarshal(data, &v); err != nil || v.ID == "" {
			return sessionStatus{}, provider.OutcomeUnknown("YouTube finished the upload but the response had no video id")
		}
		outcome := provider.OutcomePublished
		if v.Status.PrivacyStatus == provider.PrivacyPrivate {
			outcome = provider.OutcomeUnpublished
		}
		return sessionStatus{done: true, result: provider.Result{RemoteID: v.ID, URL: "https://www.youtube.com/watch?v=" + v.ID, Outcome: outcome}}, nil
	case 308:
		var next int64
		if rg := resp.Header.Get("Range"); rg != "" {
			if i := strings.LastIndex(rg, "-"); i >= 0 {
				last, err := strconv.ParseInt(rg[i+1:], 10, 64)
				if err == nil {
					next = last + 1
				}
			}
		}
		return sessionStatus{offset: next}, nil
	case http.StatusNotFound, http.StatusGone:
		return sessionStatus{}, errSessionGone
	}
	return sessionStatus{}, apiError(resp, data)
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
