package facebook

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"strconv"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

type resumeState struct {
	UploadSessionID string `json:"upload_session_id"`
	VideoID         string `json:"video_id"`
}

type phaseResponse struct {
	UploadSessionID string `json:"upload_session_id"`
	VideoID         string `json:"video_id"`
	StartOffset     string `json:"start_offset"`
	EndOffset       string `json:"end_offset"`
	Success         bool   `json:"success"`
}

// Upload uses the Page /videos chunked flow (start, transfer, finish) with the Page token.
// The finish call publishes, so it is preceded by PhaseCommitting and never repeated: a
// lost finish response is reconciled by looking the video up.
func (p *Provider) Upload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (provider.Result, error) {
	var st resumeState
	if len(req.Resume) > 0 {
		json.Unmarshal(req.Resume, &st)
	}
	pageID, token := req.AccountExternalID, req.Token.AccessToken
	if provider.PhaseAtLeast(req.Phase, provider.PhaseCommitting) {
		return p.reconcile(ctx, req, st.VideoID)
	}

	// Upload sessions are not documented as resumable across processes, so each attempt
	// before the finish call starts a new session; nothing is published until finish.
	var start phaseResponse
	if err := p.Meta.PostForm(ctx, p.Meta.VideoBase, "/"+pageID+"/videos", url.Values{
		"upload_phase": {"start"}, "file_size": {strconv.FormatInt(req.FileSize, 10)},
	}, token, &start); err != nil {
		return provider.Result{}, err
	}
	st = resumeState{UploadSessionID: start.UploadSessionID, VideoID: start.VideoID}
	if err := progress(provider.ProgressUpdate{Phase: provider.PhaseTransferring, Resume: mustJSON(st)}); err != nil {
		return provider.Result{}, err
	}

	offset, end := parseOffset(start.StartOffset), parseOffset(start.EndOffset)
	for offset < end {
		var next phaseResponse
		err := p.Meta.PostMultipart(ctx, p.Meta.VideoBase, "/"+pageID+"/videos", map[string]string{
			"upload_phase":      "transfer",
			"upload_session_id": st.UploadSessionID,
			"start_offset":      strconv.FormatInt(offset, 10),
		}, "video_file_chunk", io.NewSectionReader(req.File, offset, end-offset), token, &next)
		if err != nil {
			return provider.Result{}, err
		}
		offset, end = parseOffset(next.StartOffset), parseOffset(next.EndOffset)
		progress(provider.ProgressUpdate{BytesSent: offset})
	}

	if err := progress(provider.ProgressUpdate{BytesSent: req.FileSize, Phase: provider.PhaseCommitting, Resume: mustJSON(st)}); err != nil {
		return provider.Result{}, err
	}
	published := req.Meta.Privacy == provider.PrivacyPublic
	var fin phaseResponse
	err := p.Meta.PostForm(ctx, p.Meta.VideoBase, "/"+pageID+"/videos", url.Values{
		"upload_phase":      {"finish"},
		"upload_session_id": {st.UploadSessionID},
		"title":             {req.Meta.Title},
		"description":       {req.Meta.Description},
		"published":         {strconv.FormatBool(published)},
	}, token, &fin)
	if err != nil {
		if provider.IsPermanent(err) || ctx.Err() != nil {
			return provider.Result{}, err
		}
		return p.reconcile(ctx, req, st.VideoID)
	}
	return p.result(pageID, st.VideoID, published), nil
}

// reconcile checks whether a video whose finish call may have happened exists.
func (p *Provider) reconcile(ctx context.Context, req provider.UploadRequest, videoID string) (provider.Result, error) {
	if videoID == "" {
		return provider.Result{}, provider.OutcomeUnknown("no Facebook video id recorded for a committing job")
	}
	var v struct {
		ID        string `json:"id"`
		Published bool   `json:"published"`
		Status    struct {
			VideoStatus    string `json:"video_status"`
			UploadingPhase struct {
				Status string `json:"status"`
			} `json:"uploading_phase"`
		} `json:"status"`
	}
	err := p.Meta.Get(ctx, p.Meta.GraphBase, "/"+videoID, url.Values{"fields": {"id,published,status"}}, req.Token.AccessToken, &v)
	if err != nil || v.ID == "" || v.Status.VideoStatus == "error" || v.Status.UploadingPhase.Status != "complete" {
		return provider.Result{}, provider.OutcomeUnknown("could not confirm Facebook video %s after the finish step; check the Page's videos", videoID)
	}
	return p.result(req.AccountExternalID, videoID, v.Published), nil
}

func (p *Provider) result(pageID, videoID string, published bool) provider.Result {
	outcome := provider.OutcomeUnpublished
	if published {
		outcome = provider.OutcomePublished
	}
	return provider.Result{RemoteID: videoID, URL: "https://www.facebook.com/" + pageID + "/videos/" + videoID, Outcome: outcome}
}

func parseOffset(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
