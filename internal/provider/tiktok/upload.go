package tiktok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

const (
	chunkBytes   = 10_000_000
	uploadWindow = 55 * time.Minute // upload URLs are valid for one hour
)

type resumeState struct {
	PublishID string    `json:"publish_id"`
	UploadURL string    `json:"upload_url"`
	NextChunk int64     `json:"next_chunk"`
	StartedAt time.Time `json:"started_at"`
}

var errRestart = errors.New("upload URL rejected; start a new upload")

// chunkPlan follows the Media Transfer Guide with decimal-byte limits that hold under either
// reading of "MB": files under 10 MB go in one request; larger files use 10 MB chunks and
// the last chunk absorbs the remainder (under 20 MB).
func chunkPlan(size int64) (chunk, count int64, err error) {
	if size > maxFileBytes {
		return 0, 0, provider.Permanent("TikTok accepts files up to 4 GB; this file is %d bytes", size)
	}
	if size < chunkBytes {
		return size, 1, nil
	}
	return chunkBytes, size / chunkBytes, nil
}

// Upload sends the video to the creator's inbox. The final chunk starts TikTok's processing,
// so after it (phase committing or later) the job only polls status and never re-initializes.
func (p *Provider) Upload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (provider.Result, error) {
	var st resumeState
	if len(req.Resume) > 0 {
		json.Unmarshal(req.Resume, &st)
	}
	if provider.PhaseAtLeast(req.Phase, provider.PhaseCommitting) {
		if st.PublishID == "" {
			return provider.Result{}, provider.OutcomeUnknown("no TikTok publish_id recorded for a committing job")
		}
		return p.poll(ctx, req, st.PublishID)
	}
	resumable := req.Phase == provider.PhaseTransferring && st.PublishID != "" && time.Since(st.StartedAt) < uploadWindow
	restarts := 0
	for {
		if !resumable {
			var err error
			if st, err = p.initUpload(ctx, req, progress); err != nil {
				return provider.Result{}, err
			}
		}
		res, err := p.transfer(ctx, req, st, progress)
		if errors.Is(err, errRestart) && restarts == 0 {
			restarts++
			resumable = false
			continue
		}
		return res, err
	}
}

func (p *Provider) initUpload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (resumeState, error) {
	chunk, count, err := chunkPlan(req.FileSize)
	if err != nil {
		return resumeState{}, err
	}
	body := map[string]any{"source_info": map[string]any{
		"source": "FILE_UPLOAD", "video_size": req.FileSize, "chunk_size": chunk, "total_chunk_count": count,
	}}
	var out struct {
		PublishID string `json:"publish_id"`
		UploadURL string `json:"upload_url"`
	}
	if err := p.api(ctx, http.MethodPost, "/v2/post/publish/inbox/video/init/", req.Token, body, &out); err != nil {
		return resumeState{}, err
	}
	if out.PublishID == "" || out.UploadURL == "" {
		return resumeState{}, errors.New("TikTok init returned no publish_id or upload_url")
	}
	st := resumeState{PublishID: out.PublishID, UploadURL: out.UploadURL, StartedAt: time.Now()}
	if err := progress(provider.ProgressUpdate{BytesSent: 0, Phase: provider.PhaseTransferring, Resume: mustJSON(st)}); err != nil {
		return st, err
	}
	return st, nil
}

func (p *Provider) transfer(ctx context.Context, req provider.UploadRequest, st resumeState, progress provider.Progress) (provider.Result, error) {
	chunk, count, err := chunkPlan(req.FileSize)
	if err != nil {
		return provider.Result{}, err
	}
	for i := st.NextChunk; i < count; i++ {
		start := i * chunk
		end := start + chunk
		if i == count-1 {
			end = req.FileSize
			if err := progress(provider.ProgressUpdate{BytesSent: start, Phase: provider.PhaseCommitting, Resume: mustJSON(st)}); err != nil {
				return provider.Result{}, err
			}
		}
		code, err := p.putChunk(ctx, st.UploadURL, req, start, end)
		if err != nil {
			if i == count-1 {
				// The final chunk may have arrived; TikTok's status tells us.
				return p.poll(ctx, req, st.PublishID)
			}
			return provider.Result{}, err
		}
		switch code {
		case http.StatusPartialContent:
			st.NextChunk = i + 1
			progress(provider.ProgressUpdate{BytesSent: end, Resume: mustJSON(st)})
		case http.StatusCreated:
			if err := progress(provider.ProgressUpdate{BytesSent: req.FileSize, Phase: provider.PhaseProcessing, Resume: mustJSON(st)}); err != nil {
				return provider.Result{}, err
			}
			return p.poll(ctx, req, st.PublishID)
		case http.StatusForbidden, http.StatusNotFound, http.StatusRequestedRangeNotSatisfiable:
			// The chunk was explicitly rejected (expired URL or out-of-sync range), so the
			// upload is incomplete and no draft exists yet: starting over is safe.
			return provider.Result{}, errRestart
		default:
			if code >= 500 {
				return provider.Result{}, fmt.Errorf("TikTok upload returned HTTP %d", code)
			}
			return provider.Result{}, provider.Permanent("TikTok rejected the video chunk with HTTP %d", code)
		}
	}
	return p.poll(ctx, req, st.PublishID)
}

func (p *Provider) putChunk(ctx context.Context, uploadURL string, req provider.UploadRequest, start, end int64) (int, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodPut, uploadURL, io.NewSectionReader(req.File, start, end-start))
	if err != nil {
		return 0, err
	}
	r.ContentLength = end - start
	r.Header.Set("Content-Type", provider.MIMEType(req.Container))
	r.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end-1, req.FileSize))
	resp, err := p.HTTP.Do(r)
	if err != nil {
		return 0, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	resp.Body.Close()
	return resp.StatusCode, nil
}

// poll waits for TikTok to finish processing a publish_id.
func (p *Provider) poll(ctx context.Context, req provider.UploadRequest, publishID string) (provider.Result, error) {
	deadline := time.Now().Add(p.PollTimeout)
	var lastErr error
	for {
		var out struct {
			Status     string `json:"status"`
			FailReason string `json:"fail_reason"`
		}
		err := p.api(ctx, http.MethodPost, "/v2/post/publish/status/fetch/", req.Token, map[string]string{"publish_id": publishID}, &out)
		switch {
		case err == nil && out.Status == "SEND_TO_USER_INBOX":
			return provider.Result{RemoteID: publishID, Outcome: provider.OutcomeSentToDrafts, Note: draftNote}, nil
		case err == nil && out.Status == "PUBLISH_COMPLETE":
			return provider.Result{RemoteID: publishID, Outcome: provider.OutcomePublished}, nil
		case err == nil && out.Status == "FAILED":
			return provider.Result{}, provider.Permanent("TikTok could not process the video: %s", out.FailReason)
		case errors.Is(err, provider.ErrUnauthorized) || provider.IsPermanent(err):
			return provider.Result{}, err
		case err != nil:
			lastErr = err
		}
		if time.Now().After(deadline) {
			detail := "TikTok is still processing"
			if lastErr != nil {
				detail = lastErr.Error()
			}
			return provider.Result{}, provider.OutcomeUnknown("%s after %s; check the TikTok inbox for the draft", detail, p.PollTimeout)
		}
		select {
		case <-ctx.Done():
			return provider.Result{}, ctx.Err()
		case <-time.After(p.PollInterval):
		}
	}
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
