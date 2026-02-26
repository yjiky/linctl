package cmd

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/dorkitude/linctl/pkg/api"
	"github.com/dorkitude/linctl/pkg/auth"
	"github.com/dorkitude/linctl/pkg/output"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var issueAttachmentsCmd = &cobra.Command{
	Use:   "attachments",
	Short: "Manage issue attachments",
	Long:  "List, download, and upload attachments for a Linear issue.",
}

var issueAttachmentsListCmd = &cobra.Command{
	Use:   "list [issue-id]",
	Short: "List attachments for an issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		first, _ := cmd.Flags().GetInt("limit")
		issue, err := client.GetIssueAttachments(context.Background(), args[0], first)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to get attachments: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		attachments := []api.Attachment{}
		if issue.Attachments != nil {
			attachments = issue.Attachments.Nodes
		}

		if jsonOut {
			output.JSON(map[string]interface{}{
				"issueId":      issue.ID,
				"identifier":   issue.Identifier,
				"attachments":  attachments,
				"attachmentsCount": len(attachments),
			})
			return
		}

		table := output.TableData{
			Headers: []string{"ID", "Title", "URL", "Created"},
			Rows:    [][]string{},
		}

		for _, a := range attachments {
			created := a.CreatedAt.Format(time.RFC3339)
			table.Rows = append(table.Rows, []string{a.ID, a.Title, a.URL, created})
		}

		if plaintext {
			fmt.Printf("# Attachments for %s\n", issue.Identifier)
			output.Table(table, true, false)
			return
		}

		fmt.Printf("%s Attachments for %s\n", color.New(color.FgCyan, color.Bold).Sprint("##"), color.New(color.FgCyan, color.Bold).Sprint(issue.Identifier))
		output.Table(table, false, false)
	},
}

var issueAttachmentsDownloadCmd = &cobra.Command{
	Use:   "download [issue-id]",
	Short: "Download attachments for an issue",
	Long: `Download one or more attachments for an issue.

By default, downloads all attachments. Use --id to download only specific attachments.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		targetDir, _ := cmd.Flags().GetString("dir")
		ids, _ := cmd.Flags().GetStringSlice("id")
		limit, _ := cmd.Flags().GetInt("limit")

		issue, err := client.GetIssueAttachments(context.Background(), args[0], limit)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to get attachments: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		attachments := []api.Attachment{}
		if issue.Attachments != nil {
			attachments = issue.Attachments.Nodes
		}

		selected := attachments
		if len(ids) > 0 {
			wanted := make(map[string]struct{}, len(ids))
			for _, id := range ids {
				wanted[id] = struct{}{}
			}

			selected = []api.Attachment{}
			for _, a := range attachments {
				if _, ok := wanted[a.ID]; ok {
					selected = append(selected, a)
				}
			}

			if len(selected) == 0 {
				output.Error("No matching attachments found for provided --id values.", plaintext, jsonOut)
				os.Exit(1)
			}
		}

		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			output.Error(fmt.Sprintf("Failed to create directory '%s': %v", targetDir, err), plaintext, jsonOut)
			os.Exit(1)
		}

		httpClient := &http.Client{Timeout: 60 * time.Second}
		downloaded := []map[string]interface{}{}

		for _, a := range selected {
			outPath, err := downloadAttachment(httpClient, authHeader, targetDir, issue.Identifier, a)
			if err != nil {
				output.Error(fmt.Sprintf("Failed to download attachment %s: %v", a.ID, err), plaintext, jsonOut)
				os.Exit(1)
			}

			downloaded = append(downloaded, map[string]interface{}{
				"id":    a.ID,
				"title": a.Title,
				"url":   a.URL,
				"path":  outPath,
			})
		}

		if jsonOut {
			output.JSON(map[string]interface{}{
				"issueId":     issue.ID,
				"identifier":  issue.Identifier,
				"downloaded":  downloaded,
				"count":       len(downloaded),
				"directory":   targetDir,
			})
			return
		}

		output.Success(fmt.Sprintf("Downloaded %d attachment(s) to %s", len(downloaded), targetDir), plaintext, jsonOut)
	},
}

var issueAttachmentsUploadCmd = &cobra.Command{
	Use:   "upload [issue-id] [file-path]",
	Short: "Upload a file and attach it to an issue",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		plaintext := viper.GetBool("plaintext")
		jsonOut := viper.GetBool("json")

		authHeader, err := auth.GetAuthHeader()
		if err != nil {
			output.Error("Not authenticated. Run 'linctl auth' first.", plaintext, jsonOut)
			os.Exit(1)
		}

		client := api.NewClient(authHeader)

		issueRef := args[0]
		filePath := args[1]

		info, err := os.Stat(filePath)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to read file: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}
		if info.IsDir() {
			output.Error("File path is a directory", plaintext, jsonOut)
			os.Exit(1)
		}

		filename := filepath.Base(filePath)
		contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(filename)))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		title, _ := cmd.Flags().GetString("title")
		if title == "" {
			title = filename
		}

		issue, err := client.GetIssueAttachments(context.Background(), issueRef, 1)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to resolve issue: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		uploadFile, err := client.FileUpload(context.Background(), contentType, filename, info.Size())
		if err != nil {
			output.Error(fmt.Sprintf("Failed to request upload URL: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		if err := putFile(uploadFile.UploadURL, contentType, uploadFile.Headers, filePath); err != nil {
			output.Error(fmt.Sprintf("Failed to upload file to Linear storage: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		attachment, err := client.AttachmentCreate(context.Background(), issue.ID, title, uploadFile.AssetURL)
		if err != nil {
			output.Error(fmt.Sprintf("Failed to create attachment: %v", err), plaintext, jsonOut)
			os.Exit(1)
		}

		if jsonOut {
			output.JSON(map[string]interface{}{
				"issueId":     issue.ID,
				"identifier":  issue.Identifier,
				"attachment":  attachment,
			})
			return
		}

		output.Success(fmt.Sprintf("Attached file to %s: %s", issue.Identifier, attachment.Title), plaintext, jsonOut)
		if !plaintext {
			fmt.Printf("  URL: %s\n", color.New(color.FgCyan).Sprint(attachment.URL))
		}
	},
}

func init() {
	issueCmd.AddCommand(issueAttachmentsCmd)
	issueAttachmentsCmd.AddCommand(issueAttachmentsListCmd)
	issueAttachmentsCmd.AddCommand(issueAttachmentsDownloadCmd)
	issueAttachmentsCmd.AddCommand(issueAttachmentsUploadCmd)

	issueAttachmentsListCmd.Flags().IntP("limit", "l", 50, "Maximum attachments to fetch")

	issueAttachmentsDownloadCmd.Flags().String("dir", ".", "Directory to save downloaded files")
	issueAttachmentsDownloadCmd.Flags().StringSlice("id", nil, "Attachment ID(s) to download (if omitted, downloads all)")
	issueAttachmentsDownloadCmd.Flags().IntP("limit", "l", 50, "Maximum attachments to fetch")

	issueAttachmentsUploadCmd.Flags().String("title", "", "Attachment title (defaults to filename)")
}

func downloadAttachment(httpClient *http.Client, authHeader, targetDir, issueIdentifier string, attachment api.Attachment) (string, error) {
	u, err := url.Parse(attachment.URL)
	if err != nil {
		return "", err
	}

	filename := sanitizeFilename(attachment.Title)
	if filename == "" {
		filename = sanitizeFilename(attachment.ID)
	}

	// If title has no extension, try to infer from URL path.
	if filepath.Ext(filename) == "" {
		urlBase := path.Base(u.Path)
		if ext := filepath.Ext(urlBase); ext != "" {
			filename = filename + ext
		}
	}

	if filename == "" {
		filename = attachment.ID
	}

	outPath := filepath.Join(targetDir, fmt.Sprintf("%s-%s-%s", sanitizeFilename(issueIdentifier), sanitizeFilename(attachment.ID), filename))

	req, err := http.NewRequest("GET", attachment.URL, nil)
	if err != nil {
		return "", err
	}
	// Linear uploads are private; reuse the same bearer token.
	req.Header.Set("Authorization", authHeader)

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return "", fmt.Errorf("GET %s failed: %s: %s", u.Host, resp.Status, strings.TrimSpace(string(body)))
	}

	// If still no extension, infer from Content-Type.
	if filepath.Ext(outPath) == "" {
		ext := extFromContentType(resp.Header.Get("Content-Type"))
		if ext != "" {
			outPath = outPath + ext
		}
	}

	outFile, err := os.Create(outPath)
	if err != nil {
		return "", err
	}
	defer func() { _ = outFile.Close() }()

	if _, err := io.Copy(outFile, resp.Body); err != nil {
		return "", err
	}

	return outPath, nil
}

func putFile(uploadURL, contentType string, headers []api.UploadHeader, filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	req, err := http.NewRequest("PUT", uploadURL, f)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Cache-Control", "public, max-age=31536000")
	for _, h := range headers {
		if h.Key == "" {
			continue
		}
		req.Header.Set(h.Key, h.Value)
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		return fmt.Errorf("upload failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	return nil
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	// Replace path separators and control characters.
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\':
			return '-'
		case r < 32 || r == 127:
			return -1
		default:
			return r
		}
	}, s)

	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	s = strings.ReplaceAll(s, " ", "-")

	// Avoid extremely long filenames.
	if len(s) > 120 {
		s = s[:120]
	}

	return s
}

func extFromContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if ct == "" {
		return ""
	}
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}

	switch ct {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "application/zip":
		return ".zip"
	default:
		return ""
	}
}
