package instagram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nhatnam1507/social-mcp/internal/provider"
)

type resumeState struct {
	ContainerID string `json:"container_id"`
}

type containerStatus struct {
	StatusCode  string `json:"status_code"`
	VideoStatus struct {
		UploadingPhase struct {
			Status           string `json:"status"`
			BytesTransferred int64  `json:"bytes_transferred"`
		} `json:"uploading_phase"`
	} `json:"video_status"`
}

// Upload creates a resumable Reels container, uploads the file, waits for processing and
// publishes. media_publish is the commit step: after PhaseCommitting it is only repeated if
// the container is confirmed unpublished.
func (p *Provider) Upload(ctx context.Context, req provider.UploadRequest, progress provider.Progress) (provider.Result, error) {
	var st resumeState
	if len(req.Resume) > 0 {
		json.Unmarshal(req.Resume, &st)
	}
	if provider.PhaseAtLeast(req.Phase, provider.PhaseCommitting) {
		return p.reconcile(ctx, req, st.ContainerID, true)
	}

	offset := int64(0)
	if st.ContainerID != "" {
		s, err := p.status(ctx, req, st.ContainerID)
		switch {
		case err != nil && !provider.IsPermanent(err):
			return provider.Result{}, err
		case err != nil || s.StatusCode == "ERROR" || s.StatusCode == "EXPIRED":
			st = resumeState{} // unusable container; nothing was published
		case s.StatusCode == "PUBLISHED":
			return p.finish(ctx, req, "")
		case s.StatusCode == "FINISHED":
			offset = req.FileSize
		default:
			offset = s.VideoStatus.UploadingPhase.BytesTransferred
		}
	}
	if st.ContainerID == "" {
		if err := p.checkQuota(ctx, req); err != nil {
			return provider.Result{}, err
		}
		id, err := p.createContainer(ctx, req)
		if err != nil {
			return provider.Result{}, err
		}
		st = resumeState{ContainerID: id}
		if err := progress(provider.ProgressUpdate{Phase: provider.PhaseTransferring, Resume: mustJSON(st)}); err != nil {
			return provider.Result{}, err
		}
	}
	if offset < req.FileSize {
		if err := p.upload(ctx, req, st.ContainerID, offset); err != nil {
			return provider.Result{}, err
		}
		progress(provider.ProgressUpdate{BytesSent: req.FileSize})
	}
	if err := p.waitFinished(ctx, req, st.ContainerID); err != nil {
		return provider.Result{}, err
	}
	if err := progress(provider.ProgressUpdate{BytesSent: req.FileSize, Phase: provider.PhaseCommitting, Resume: mustJSON(st)}); err != nil {
		return provider.Result{}, err
	}
	return p.publish(ctx, req, st.ContainerID)
}

func (p *Provider) checkQuota(ctx context.Context, req provider.UploadRequest) error {
	var out struct {
		Data []struct {
			QuotaUsage int `json:"quota_usage"`
			Config     struct {
				QuotaTotal int `json:"quota_total"`
			} `json:"config"`
		} `json:"data"`
	}
	if err := p.Meta.Get(ctx, p.Meta.GraphBase, "/"+req.AccountExternalID+"/content_publishing_limit", url.Values{"fields": {"quota_usage,config"}}, req.Token.AccessToken, &out); err != nil {
		return err
	}
	if len(out.Data) > 0 && out.Data[0].Config.QuotaTotal > 0 && out.Data[0].QuotaUsage >= out.Data[0].Config.QuotaTotal {
		return provider.Permanent("Instagram publishing limit reached (%d of %d posts in 24 hours); try again later", out.Data[0].QuotaUsage, out.Data[0].Config.QuotaTotal)
	}
	return nil
}

func (p *Provider) createContainer(ctx context.Context, req provider.UploadRequest) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	form := url.Values{
		"media_type":    {"REELS"},
		"upload_type":   {"resumable"},
		"caption":       {caption(req.Meta)},
		"share_to_feed": {strconv.FormatBool(provider.BoolOption(req.Meta.Options, "share_to_feed", true))},
	}
	if err := p.Meta.PostForm(ctx, p.Meta.GraphBase, "/"+req.AccountExternalID+"/media", form, req.Token.AccessToken, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", errors.New("Instagram returned no container id")
	}
	return out.ID, nil
}

// upload sends the file from offset to rupload.facebook.com.
func (p *Provider) upload(ctx context.Context, req provider.UploadRequest, containerID string, offset int64) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Meta.RuploadBase+"/"+containerID, io.NewSectionReader(req.File, offset, req.FileSize-offset))
	if err != nil {
		return err
	}
	r.ContentLength = req.FileSize - offset
	r.Header.Set("Authorization", "OAuth "+req.Token.AccessToken)
	r.Header.Set("offset", strconv.FormatInt(offset, 10))
	r.Header.Set("file_size", strconv.FormatInt(req.FileSize, 10))
	resp, err := p.HTTP.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Success   bool `json:"success"`
		DebugInfo struct {
			Retriable bool   `json:"retriable"`
			Type      string `json:"type"`
			Message   string `json:"message"`
		} `json:"debug_info"`
	}
	json.Unmarshal(body, &out)
	switch {
	case resp.StatusCode == http.StatusOK && out.Success:
		return nil
	case resp.StatusCode >= 500 || out.DebugInfo.Retriable:
		return fmt.Errorf("Instagram upload failed (%s): %s", resp.Status, out.DebugInfo.Message)
	}
	return provider.Permanent("Instagram rejected the upload (%s): %s %s", resp.Status, out.DebugInfo.Type, out.DebugInfo.Message)
}

func (p *Provider) status(ctx context.Context, req provider.UploadRequest, containerID string) (containerStatus, error) {
	var s containerStatus
	err := p.Meta.Get(ctx, p.Meta.GraphBase, "/"+containerID, url.Values{"fields": {"status_code,video_status"}}, req.Token.AccessToken, &s)
	return s, err
}

// waitFinished polls until Instagram has processed the container. Nothing is published yet,
// so a timeout is an ordinary retryable failure.
func (p *Provider) waitFinished(ctx context.Context, req provider.UploadRequest, containerID string) error {
	deadline := time.Now().Add(p.PollTimeout)
	for {
		s, err := p.status(ctx, req, containerID)
		if err != nil && (provider.IsPermanent(err) || errors.Is(err, provider.ErrReauthRequired)) {
			return err
		}
		switch s.StatusCode {
		case "FINISHED", "PUBLISHED":
			return nil
		case "ERROR", "EXPIRED":
			return provider.Permanent("Instagram could not process the video (container status %s); check the Reels video requirements", s.StatusCode)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("Instagram is still processing the video after %s", p.PollTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.PollInterval):
		}
	}
}

func (p *Provider) publish(ctx context.Context, req provider.UploadRequest, containerID string) (provider.Result, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := p.Meta.PostForm(ctx, p.Meta.GraphBase, "/"+req.AccountExternalID+"/media_publish", url.Values{"creation_id": {containerID}}, req.Token.AccessToken, &out)
	if err != nil {
		if provider.IsPermanent(err) || errors.Is(err, provider.ErrReauthRequired) || ctx.Err() != nil {
			return provider.Result{}, err
		}
		return p.reconcile(ctx, req, containerID, false)
	}
	return p.finish(ctx, req, out.ID)
}

// reconcile decides what to do with a container whose publish may have happened. With
// mayPublish, a container confirmed FINISHED (not published) is published now.
func (p *Provider) reconcile(ctx context.Context, req provider.UploadRequest, containerID string, mayPublish bool) (provider.Result, error) {
	if containerID == "" {
		return provider.Result{}, provider.OutcomeUnknown("no Instagram container recorded for a committing job")
	}
	s, err := p.status(ctx, req, containerID)
	switch {
	case err == nil && s.StatusCode == "PUBLISHED":
		return p.finish(ctx, req, "")
	case err == nil && s.StatusCode == "FINISHED" && mayPublish:
		// Confirmed not yet published: publishing now cannot duplicate.
		return p.publish(ctx, req, containerID)
	}
	return provider.Result{}, provider.OutcomeUnknown("could not confirm whether Instagram published container %s; check the profile", containerID)
}

func (p *Provider) finish(ctx context.Context, req provider.UploadRequest, mediaID string) (provider.Result, error) {
	res := provider.Result{RemoteID: mediaID, Outcome: provider.OutcomePublished}
	if mediaID == "" {
		res.Note = "Published; the permalink is not available. Check the Instagram profile."
		return res, nil
	}
	var out struct {
		Permalink string `json:"permalink"`
	}
	if err := p.Meta.Get(ctx, p.Meta.GraphBase, "/"+mediaID, url.Values{"fields": {"permalink"}}, req.Token.AccessToken, &out); err == nil {
		res.URL = out.Permalink
	}
	return res, nil
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
