package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/nhatnam1507/social-mcp/internal/logx"
	"github.com/nhatnam1507/social-mcp/internal/service"
)

type listAccountsIn struct {
	Platform string `json:"platform,omitempty" jsonschema:"only list accounts on this platform: youtube, tiktok, facebook or instagram"`
}

type listAccountsOut struct {
	Accounts []service.AccountView `json:"accounts"`
}

type uploadVideoIn struct {
	FilePath        string                    `json:"file_path" jsonschema:"absolute path of the local video file (MP4, MOV, WebM or MKV)"`
	AccountIDs      []string                  `json:"account_ids" jsonschema:"account_id values from list_accounts to upload to; one job is created per account"`
	Title           string                    `json:"title" jsonschema:"video title"`
	Description     string                    `json:"description,omitempty" jsonschema:"video description or caption"`
	Tags            []string                  `json:"tags,omitempty" jsonschema:"tags or hashtags without the # sign"`
	Privacy         string                    `json:"privacy,omitempty" jsonschema:"private (default), unlisted or public; must be in the account's capabilities.privacy_levels"`
	PlatformOptions map[string]map[string]any `json:"platform_options,omitempty" jsonschema:"per-platform options keyed by platform name; allowed keys are listed in each account's options"`
}

type jobIn struct {
	JobID string `json:"job_id" jsonschema:"job_id returned by upload_video"`
}

type listUploadsIn struct {
	Status    string `json:"status,omitempty" jsonschema:"filter by status: queued, uploading, succeeded, failed, cancelled or needs_review"`
	AccountID string `json:"account_id,omitempty" jsonschema:"filter by account_id"`
	Limit     int    `json:"limit,omitempty" jsonschema:"maximum jobs to return (default 20, max 100)"`
}

type listUploadsOut struct {
	Jobs []service.JobView `json:"jobs"`
}

func addTools(s *mcp.Server, svc service.Service) {
	yes, no := true, false

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_accounts",
		Title:       "List connected accounts",
		Description: "List the connected social media accounts with their platform, health, privacy levels, restrictions and accepted options. Call this before upload_video.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listAccountsIn) (*mcp.CallToolResult, listAccountsOut, error) {
		accs, err := svc.ListAccounts(in.Platform)
		if accs == nil {
			accs = []service.AccountView{}
		}
		return nil, listAccountsOut{Accounts: accs}, clean(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "upload_video",
		Title: "Upload a video",
		Description: "Publish a local video file to one or more connected accounts on external platforms (YouTube, TikTok drafts, Facebook Pages, Instagram Reels) " +
			"with the given privacy. Returns one job per accepted account plus the accounts that were rejected and why. Uploads continue in the background; poll get_upload_status.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &yes, IdempotentHint: false, OpenWorldHint: &yes},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in uploadVideoIn) (*mcp.CallToolResult, service.UploadSubmission, error) {
		sub, err := svc.SubmitUpload(service.UploadInput{
			FilePath: in.FilePath, AccountIDs: in.AccountIDs, Title: in.Title, Description: in.Description,
			Tags: in.Tags, Privacy: in.Privacy, PlatformOptions: in.PlatformOptions,
		})
		return nil, sub, clean(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_upload_status",
		Title:       "Get upload status",
		Description: "Get the status, progress, outcome and URL of an upload job.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIn) (*mcp.CallToolResult, service.JobView, error) {
		job, err := svc.GetUpload(in.JobID)
		return nil, job, clean(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_uploads",
		Title:       "List uploads",
		Description: "List recent upload jobs, newest first.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in listUploadsIn) (*mcp.CallToolResult, listUploadsOut, error) {
		jobs, err := svc.ListUploads(service.UploadFilter{Status: in.Status, AccountID: in.AccountID, Limit: in.Limit})
		if jobs == nil {
			jobs = []service.JobView{}
		}
		return nil, listUploadsOut{Jobs: jobs}, clean(err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "cancel_upload",
		Title:       "Cancel an upload",
		Description: "Cancel a queued or running upload. Uploads that are already being published cannot be cancelled.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &no, IdempotentHint: true, OpenWorldHint: &no},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in jobIn) (*mcp.CallToolResult, service.JobView, error) {
		job, err := svc.CancelUpload(in.JobID)
		return nil, job, clean(err)
	})
}

// clean turns service errors into sanitized tool errors.
func clean(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, service.ErrNotFound):
		return errors.New("not found")
	default:
		return errors.New(logx.Error(err))
	}
}
