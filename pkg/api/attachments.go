package api

import (
	"context"
	"fmt"
)

type UploadHeader struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type UploadFile struct {
	UploadURL string         `json:"uploadUrl"`
	AssetURL  string         `json:"assetUrl"`
	Headers   []UploadHeader `json:"headers"`
}

// FileUpload requests a pre-signed upload URL for Linear file storage.
func (c *Client) FileUpload(ctx context.Context, contentType, filename string, size int64) (*UploadFile, error) {
	query := `
		mutation FileUpload($contentType: String!, $filename: String!, $size: Int!) {
			fileUpload(contentType: $contentType, filename: $filename, size: $size) {
				success
				uploadFile {
					uploadUrl
					assetUrl
					headers {
						key
						value
					}
				}
			}
		}
	`

	variables := map[string]interface{}{
		"contentType": contentType,
		"filename":    filename,
		"size":        int(size),
	}

	var response struct {
		FileUpload struct {
			Success    bool        `json:"success"`
			UploadFile *UploadFile `json:"uploadFile"`
		} `json:"fileUpload"`
	}

	if err := c.Execute(ctx, query, variables, &response); err != nil {
		return nil, err
	}

	if !response.FileUpload.Success || response.FileUpload.UploadFile == nil {
		return nil, fmt.Errorf("fileUpload failed")
	}

	return response.FileUpload.UploadFile, nil
}

// AttachmentCreate creates (or updates) an attachment on an issue.
// Linear treats attachments as idempotent by URL per issue.
func (c *Client) AttachmentCreate(ctx context.Context, issueID, title, url string) (*Attachment, error) {
	query := `
		mutation AttachmentCreate($input: AttachmentCreateInput!) {
			attachmentCreate(input: $input) {
				success
				attachment {
					id
					title
					subtitle
					url
					metadata
					createdAt
					creator {
						id
						name
						email
					}
				}
			}
		}
	`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"issueId": issueID,
			"title":   title,
			"url":     url,
		},
	}

	var response struct {
		AttachmentCreate struct {
			Success    bool        `json:"success"`
			Attachment *Attachment `json:"attachment"`
		} `json:"attachmentCreate"`
	}

	if err := c.Execute(ctx, query, variables, &response); err != nil {
		return nil, err
	}

	if !response.AttachmentCreate.Success || response.AttachmentCreate.Attachment == nil {
		return nil, fmt.Errorf("attachmentCreate failed")
	}

	return response.AttachmentCreate.Attachment, nil
}

// GetIssueAttachments returns attachments for an issue (by UUID or identifier).
func (c *Client) GetIssueAttachments(ctx context.Context, issueRef string, first int) (*Issue, error) {
	query := `
		query IssueAttachments($id: String!, $first: Int!) {
			issue(id: $id) {
				id
				identifier
				attachments(first: $first) {
					nodes {
						id
						title
						subtitle
						url
						metadata
						createdAt
						creator {
							id
							name
							email
						}
					}
				}
			}
		}
	`

	if first <= 0 {
		first = 50
	}

	variables := map[string]interface{}{
		"id":    issueRef,
		"first": first,
	}

	var response struct {
		Issue Issue `json:"issue"`
	}

	if err := c.Execute(ctx, query, variables, &response); err != nil {
		return nil, err
	}

	if response.Issue.ID == "" {
		return nil, fmt.Errorf("issue not found: %s", issueRef)
	}

	return &response.Issue, nil
}
