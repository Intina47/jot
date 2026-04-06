package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	gmailAPIBaseURL        = "https://gmail.googleapis.com"
	gmailDeviceCodeURL     = "https://oauth2.googleapis.com/device/code"
	gmailTokenURL          = "https://oauth2.googleapis.com/token"
	gmailVerificationURL   = "https://accounts.google.com/device"
	gmailDefaultTimeout    = 30 * time.Second
	gmailTokenRefreshSlack = 60 * time.Second
	gmailFetchConcurrency  = 6
)

var gmailRequiredScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/calendar",
}

type GmailCapability struct {
	Config            AssistantConfig
	TokenPath         string
	CredPath          string
	AttachmentSaveDir string
	BaseURL           string
	Client            *http.Client
	ProgressFn        func(string)
	AuthFn            func(io.Writer) error

	mu       sync.Mutex
	creds    *gmailOAuthCredentials
	token    *gmailOAuthToken
	email    string
	verified bool
}

func (g *GmailCapability) SetProgressReporter(fn func(string)) {
	g.mu.Lock()
	g.ProgressFn = fn
	g.mu.Unlock()
}

type gmailOAuthCredentials struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret,omitempty"`
	DeviceURL    string   `json:"device_url,omitempty"`
	TokenURL     string   `json:"token_url,omitempty"`
	Scopes       []string `json:"scopes,omitempty"`
}

type gmailOAuthToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenType    string    `json:"token_type,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	Expiry       time.Time `json:"expiry"`
}

type gmailDeviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURL         string `json:"verification_url"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
	Error                   string `json:"error"`
	ErrorDescription        string `json:"error_description"`
}

type gmailTokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type gmailProfileResponse struct {
	EmailAddress  string `json:"emailAddress"`
	MessagesTotal int    `json:"messagesTotal"`
	ThreadsTotal  int    `json:"threadsTotal"`
	HistoryID     string `json:"historyId"`
}

type gmailListMessagesResponse struct {
	Messages           []gmailMessageRef `json:"messages"`
	NextPageToken      string            `json:"nextPageToken"`
	ResultSizeEstimate int               `json:"resultSizeEstimate"`
}

type gmailMessageRef struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type gmailMessage struct {
	ID           string           `json:"id"`
	ThreadID     string           `json:"threadId"`
	LabelIDs     []string         `json:"labelIds"`
	Snippet      string           `json:"snippet"`
	HistoryID    string           `json:"historyId"`
	InternalDate string           `json:"internalDate"`
	Payload      gmailMessagePart `json:"payload"`
	SizeEstimate int64            `json:"sizeEstimate"`
}

type gmailMessagePart struct {
	PartID   string             `json:"partId"`
	MimeType string             `json:"mimeType"`
	Filename string             `json:"filename"`
	Body     gmailMessageBody   `json:"body"`
	Headers  []gmailHeader      `json:"headers"`
	Parts    []gmailMessagePart `json:"parts"`
}

type gmailMessageBody struct {
	Size         int64  `json:"size"`
	Data         string `json:"data"`
	AttachmentID string `json:"attachmentId"`
}

type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type gmailAttachmentResponse struct {
	AttachmentID string `json:"attachmentId"`
	Size         int64  `json:"size"`
	Data         string `json:"data"`
}

type gmailDraftRequest struct {
	Message gmailRawMessage `json:"message"`
}

type gmailRawMessage struct {
	Raw string `json:"raw"`
}

type gmailDraftResponse struct {
	ID      string       `json:"id"`
	Message gmailMessage `json:"message"`
}

type gmailSendResponse struct {
	ID string `json:"id"`
}

type gmailAttachmentDownloadResult struct {
	SavedPath string                        `json:"savedPath"`
	Filename  string                        `json:"filename"`
	Bytes     int64                         `json:"bytes"`
	Count     int                           `json:"count,omitempty"`
	MessageID string                        `json:"messageId,omitempty"`
	ThreadID  string                        `json:"threadId,omitempty"`
	Subject   string                        `json:"subject,omitempty"`
	From      string                        `json:"from,omitempty"`
	Date      time.Time                     `json:"date,omitempty"`
	Files     []gmailAttachmentDownloadFile `json:"files,omitempty"`
}

type gmailAttachmentDownloadFile struct {
	MessageID    string    `json:"messageId,omitempty"`
	ThreadID     string    `json:"threadId,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	From         string    `json:"from,omitempty"`
	Date         time.Time `json:"date,omitempty"`
	Filename     string    `json:"filename"`
	MimeType     string    `json:"mimeType,omitempty"`
	AttachmentID string    `json:"attachmentId"`
	SavedPath    string    `json:"savedPath"`
	Bytes        int64     `json:"bytes"`
}

type gmailAttachmentContentResult struct {
	MessageID  string            `json:"messageId,omitempty"`
	ThreadID   string            `json:"threadId,omitempty"`
	Subject    string            `json:"subject,omitempty"`
	From       string            `json:"from,omitempty"`
	Date       time.Time         `json:"date,omitempty"`
	Attachment AttachmentMeta    `json:"attachment"`
	Content    AttachmentContent `json:"content"`
	Preview    string            `json:"preview,omitempty"`
	Readable   bool              `json:"readable"`
	Error      string            `json:"error,omitempty"`
}

type gmailIndexedAttachmentSelection struct {
	Index     int
	Total     int
	Selection gmailAttachmentSelection
}

type gmailThreadResult struct {
	ThreadID        string            `json:"threadId"`
	Subject         string            `json:"subject,omitempty"`
	Participants    []string          `json:"participants,omitempty"`
	MessageCount    int               `json:"messageCount,omitempty"`
	AttachmentCount int               `json:"attachmentCount,omitempty"`
	EarliestDate    time.Time         `json:"earliestDate,omitempty"`
	LatestDate      time.Time         `json:"latestDate,omitempty"`
	Messages        []NormalizedEmail `json:"messages"`
}

type gmailLabelMutationRequest struct {
	AddLabelIDs    []string `json:"addLabelIds,omitempty"`
	RemoveLabelIDs []string `json:"removeLabelIds,omitempty"`
}

type gmailLabelMutationTarget struct {
	Kind         string    `json:"kind"`
	ThreadID     string    `json:"threadId,omitempty"`
	MessageID    string    `json:"messageId,omitempty"`
	Subject      string    `json:"subject,omitempty"`
	From         string    `json:"from,omitempty"`
	Date         time.Time `json:"date,omitempty"`
	MessageCount int       `json:"messageCount,omitempty"`
	Participants []string  `json:"participants,omitempty"`
	Unread       bool      `json:"unread,omitempty"`
}

func NewGmailCapability(cfg AssistantConfig) (*GmailCapability, error) {
	tokenPath, err := gmailResolveTokenPath(cfg.GmailTokenPath)
	if err != nil {
		return nil, err
	}
	credPath, err := gmailResolveCredentialPath(cfg.GmailCredPath)
	if err != nil {
		return nil, err
	}
	cap := &GmailCapability{
		Config:            cfg,
		TokenPath:         tokenPath,
		CredPath:          credPath,
		AttachmentSaveDir: strings.TrimSpace(cfg.AttachmentSaveDir),
		BaseURL:           gmailAPIBaseURL,
	}
	if creds, err := gmailLoadOAuthCredentials(cap.CredPath); err == nil {
		cap.creds = creds
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if token, err := gmailLoadOAuthToken(cap.TokenPath); err == nil {
		cap.token = token
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	cap.Client = cap.authenticatedHTTPClient()
	return cap, nil
}

func (g *GmailCapability) Name() string { return "gmail" }

func (g *GmailCapability) Description() string {
	return "Read, search, and act on Gmail"
}

func (g *GmailCapability) Tools() []Tool {
	return []Tool{
		{Name: "gmail.status", Description: "Check whether Gmail is connected and report the connected address", ParamSchema: `{}`},
		{Name: "gmail.search", Description: "Search Gmail messages with a Gmail query string or a natural-language fallback", ParamSchema: `{"type":"object","properties":{"query":{"type":"string"},"input":{"type":"string"},"max":{"type":"integer","minimum":1}}}`},
		{Name: "gmail.read_message", Description: "Fetch one Gmail message and normalize its body to plain text", ParamSchema: `{"type":"object","properties":{"id":{"type":"string"}},"required":["id"]}`},
		{Name: "gmail.read_thread", Description: "Fetch a Gmail thread by thread id and normalize every message with thread context", ParamSchema: `{"type":"object","properties":{"id":{"type":"string"},"thread_id":{"type":"string"}}}`},
		{Name: "gmail.fill_form", Description: "Inspect a form with the browser computer, gather answers from direct user instructions plus any available email context, and guide the user through review and browser-assisted filling. Accepts a direct form_url or an email message/thread reference.", ParamSchema: `{"type":"object","properties":{"message_id":{"type":"string"},"thread_id":{"type":"string"},"form_url":{"type":"string"}}}`},
		{Name: "gmail.list_attachments", Description: "List attachment metadata for one message or a whole thread", ParamSchema: `{"type":"object","properties":{"message_id":{"type":"string"},"thread_id":{"type":"string"},"id":{"type":"string"}}}`},
		{Name: "gmail.read_attachment", Description: "Read and extract text from one or more Gmail attachments without saving them to disk", ParamSchema: `{"type":"object","properties":{"message_id":{"type":"string"},"thread_id":{"type":"string"},"attachment_id":{"type":"string"},"attachment_ids":{"type":"array","items":{"type":"string"}},"filename":{"type":"string"},"filenames":{"type":"array","items":{"type":"string"}},"read_all":{"type":"boolean"},"all":{"type":"boolean"},"max_attachments":{"type":"integer","minimum":1}}}`},
		{Name: "gmail.download_attachment", Description: "Download one attachment, or all matching attachments from a message or thread, to disk", ParamSchema: `{"type":"object","properties":{"message_id":{"type":"string"},"thread_id":{"type":"string"},"attachment_id":{"type":"string"},"attachment_ids":{"type":"array","items":{"type":"string"}},"filename":{"type":"string"},"filenames":{"type":"array","items":{"type":"string"}},"save_dir":{"type":"string"},"download_all":{"type":"boolean"},"all":{"type":"boolean"}}}`},
		{Name: "gmail.archive_thread", Description: "Archive a Gmail thread, preferring thread context and accepting a message id when needed", ParamSchema: `{"type":"object","properties":{"thread_id":{"type":"string"},"message_id":{"type":"string"},"id":{"type":"string"}}}`},
		{Name: "gmail.mark_read", Description: "Mark a Gmail thread or message as read, preferring thread context", ParamSchema: `{"type":"object","properties":{"thread_id":{"type":"string"},"message_id":{"type":"string"},"id":{"type":"string"}}}`},
		{Name: "gmail.star_thread", Description: "Star a Gmail thread or message, preferring thread context", ParamSchema: `{"type":"object","properties":{"thread_id":{"type":"string"},"message_id":{"type":"string"},"id":{"type":"string"}}}`},
		{Name: "gmail.extract_actions", Description: "Extract action items, deadlines, meeting requests, and entities from message text", ParamSchema: `{"type":"object","properties":{"text":{"type":"string"},"message_id":{"type":"string"}}}`},
		{Name: "gmail.draft_reply", Description: "Compose a Gmail reply draft from a message or thread; send is supported behind confirmation", ParamSchema: `{"type":"object","properties":{"message_id":{"type":"string"},"thread_id":{"type":"string"},"body":{"type":"string"},"send":{"type":"boolean"},"experimental":{"type":"boolean"}},"required":["body"]}`},
		{Name: "gmail.send_email", Description: "Compose a brand new Gmail email and either draft it or send it; supports file attachments and sending behind confirmation", ParamSchema: `{"type":"object","properties":{"to":{"type":"string"},"subject":{"type":"string"},"body":{"type":"string"},"attachment_path":{"type":"string"},"attachment_paths":{"type":"array","items":{"type":"string"}},"send":{"type":"boolean"}},"required":["to","subject","body"]}`},
	}
}

func (g *GmailCapability) Execute(toolName string, params map[string]any) (ToolResult, error) {
	switch toolName {
	case "gmail.status":
		return g.executeStatus()
	case "gmail.search":
		return g.executeSearch(params)
	case "gmail.read_message":
		return g.executeReadMessage(params)
	case "gmail.read_thread":
		return g.executeReadThread(params)
	case "gmail.fill_form":
		return ToolResult{Success: false, Error: "gmail.fill_form is handled by the assistant runtime"}, errors.New("gmail.fill_form is handled by the assistant runtime")
	case "gmail.list_attachments":
		return g.executeListAttachments(params)
	case "gmail.read_attachment":
		return g.executeReadAttachment(params)
	case "gmail.download_attachment":
		return g.executeDownloadAttachment(params)
	case "gmail.archive_thread":
		return g.executeArchiveThread(params)
	case "gmail.mark_read":
		return g.executeMarkRead(params)
	case "gmail.star_thread":
		return g.executeStarThread(params)
	case "gmail.extract_actions":
		return g.executeExtractActions(params)
	case "gmail.draft_reply":
		return g.executeDraftReply(params)
	case "gmail.send_email":
		return g.executeSendEmail(params)
	default:
		return ToolResult{Success: false, Error: fmt.Sprintf("unknown gmail tool %q", toolName)}, fmt.Errorf("unknown gmail tool %q", toolName)
	}
}

func gmailAuth(w io.Writer, cfg AssistantConfig) error {
	cap, err := NewGmailCapability(cfg)
	if err != nil {
		return err
	}
	return cap.Authenticate(w)
}

func (g *GmailCapability) Authenticate(w io.Writer) error {
	if g.AuthFn != nil {
		return g.AuthFn(w)
	}
	creds, err := g.loadOrCreateCredentials()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("gmail OAuth client credentials are not configured; set JOT_GMAIL_CLIENT_ID and JOT_GMAIL_CLIENT_SECRET, or create %s", g.CredPath)
		}
		return err
	}
	redirectURI, authURL, state, codeVerifier, listener, codeCh, err := g.startAuthFlow(creds)
	if err != nil {
		return err
	}
	defer listener.Close()
	if _, err := fmt.Fprintf(w, "open %s\n", authURL); err != nil {
		return err
	}
	_ = openURLInBrowser(authURL)

	code, returnedState, err := g.waitForAuthCode(codeCh)
	if err != nil {
		return err
	}
	if returnedState != state {
		return errors.New("gmail auth state mismatch")
	}

	token, err := g.exchangeAuthCode(creds, code, redirectURI, codeVerifier)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.token = token
	g.creds = creds
	g.email = ""
	g.verified = true
	g.mu.Unlock()

	if err := gmailSaveOAuthCredentials(g.CredPath, creds); err != nil {
		return err
	}
	if err := gmailSaveOAuthToken(g.TokenPath, token); err != nil {
		return err
	}

	g.Client = g.authenticatedHTTPClient()
	profile, err := g.profile()
	if err == nil {
		g.mu.Lock()
		g.email = profile.EmailAddress
		g.mu.Unlock()
		if profile.EmailAddress != "" {
			_, _ = fmt.Fprintf(w, "connected as %s\n", profile.EmailAddress)
		}
		return nil
	}
	_, _ = fmt.Fprintln(w, "connected")
	return nil
}

func (g *GmailCapability) startAuthFlow(creds *gmailOAuthCredentials) (string, string, string, string, net.Listener, chan authCallbackResult, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", "", "", nil, nil, err
	}
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/oauth2callback", listener.Addr().(*net.TCPAddr).Port)

	state, err := gmailRandomToken(24)
	if err != nil {
		listener.Close()
		return "", "", "", "", nil, nil, err
	}
	codeVerifier, err := gmailRandomToken(48)
	if err != nil {
		listener.Close()
		return "", "", "", "", nil, nil, err
	}
	sum := sha256.Sum256([]byte(codeVerifier))
	codeChallenge := base64.RawURLEncoding.EncodeToString(sum[:])

	query := url.Values{}
	query.Set("client_id", creds.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(creds.ScopesOrDefault(), " "))
	query.Set("access_type", "offline")
	query.Set("prompt", "consent")
	query.Set("include_granted_scopes", "true")
	query.Set("state", state)
	query.Set("code_challenge", codeChallenge)
	query.Set("code_challenge_method", "S256")
	authURL := "https://accounts.google.com/o/oauth2/v2/auth?" + query.Encode()

	codeCh := make(chan authCallbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}
	mux.HandleFunc("/oauth2callback", func(w http.ResponseWriter, r *http.Request) {
		result := authCallbackResult{
			Code:  strings.TrimSpace(r.URL.Query().Get("code")),
			State: strings.TrimSpace(r.URL.Query().Get("state")),
			Error: strings.TrimSpace(r.URL.Query().Get("error")),
		}
		select {
		case codeCh <- result:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if result.Error != "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, "<html><body><p>Gmail authorization failed. You can return to Jot.</p></body></html>")
			return
		}
		_, _ = io.WriteString(w, "<html><body><p>Gmail connected. You can return to Jot.</p></body></html>")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
		}()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	return redirectURI, authURL, state, codeVerifier, listener, codeCh, nil
}

type authCallbackResult struct {
	Code  string
	State string
	Error string
}

func (g *GmailCapability) waitForAuthCode(codeCh <-chan authCallbackResult) (string, string, error) {
	select {
	case result := <-codeCh:
		if result.Error != "" {
			return "", "", fmt.Errorf("gmail authorization failed: %s", result.Error)
		}
		if strings.TrimSpace(result.Code) == "" {
			return "", "", errors.New("gmail authorization did not return a code")
		}
		return result.Code, result.State, nil
	case <-time.After(5 * time.Minute):
		return "", "", errors.New("timed out waiting for gmail authorization")
	}
}

func (g *GmailCapability) exchangeAuthCode(creds *gmailOAuthCredentials, code string, redirectURI string, codeVerifier string) (*gmailOAuthToken, error) {
	form := url.Values{}
	form.Set("client_id", creds.ClientID)
	form.Set("code", code)
	form.Set("code_verifier", codeVerifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)
	if strings.TrimSpace(creds.ClientSecret) != "" {
		form.Set("client_secret", creds.ClientSecret)
	}

	endpoint := creds.TokenURL
	if endpoint == "" {
		endpoint = gmailTokenURL
	}
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp gmailTokenResponse
	if err := gmailDecodeResponse(resp, &tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, errors.New("authorization code exchange did not return an access token")
	}
	return &gmailOAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
		Expiry:       time.Now().Add(time.Duration(max(tokenResp.ExpiresIn, 0)) * time.Second),
	}, nil
}

func gmailRandomToken(byteCount int) (string, error) {
	if byteCount <= 0 {
		byteCount = 32
	}
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (g *GmailCapability) executeStatus() (ToolResult, error) {
	profile, err := g.profile()
	if err != nil {
		return ToolResult{
			Success: true,
			Data: map[string]any{
				"connected": false,
				"email":     "",
			},
			Text: "gmail: not connected",
		}, nil
	}
	return ToolResult{
		Success: true,
		Data: map[string]any{
			"connected":  true,
			"email":      profile.EmailAddress,
			"sendReady":  g.sendScopeAvailable(),
			"tokenScope": g.tokenScopeSummary(),
		},
		Text: fmt.Sprintf("gmail: connected (%s)", profile.EmailAddress),
	}, nil
}

func (g *GmailCapability) executeSearch(params map[string]any) (ToolResult, error) {
	query := paramString(params, "query", "q", "input")
	if strings.TrimSpace(query) == "" {
		query = mapNLToGmailQuery(paramString(params, "input"))
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return ToolResult{Success: false, Error: "query must be provided"}, errors.New("query must be provided")
	}
	maxResults := paramInt(params, 10, "max", "limit")
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 50 {
		maxResults = 50
	}

	messages, err := g.searchMessages(query, maxResults)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	summaries := make([]string, 0, len(messages))
	for i, msg := range messages {
		summaries = append(summaries, fmt.Sprintf("%d. %s", i+1, gmailOneLineSummary(msg)))
	}
	return ToolResult{
		Success: true,
		Data:    messages,
		Text:    strings.Join(summaries, "\n"),
	}, nil
}

func (g *GmailCapability) executeReadMessage(params map[string]any) (ToolResult, error) {
	id := paramString(params, "id", "message_id")
	if id == "" {
		return ToolResult{Success: false, Error: "id must be provided"}, errors.New("id must be provided")
	}
	msg, err := g.readMessage(id)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return ToolResult{Success: true, Data: msg, Text: gmailOneLineSummary(msg)}, nil
}

func (g *GmailCapability) executeReadThread(params map[string]any) (ToolResult, error) {
	id := paramString(params, "id", "thread_id")
	if id == "" {
		return ToolResult{Success: false, Error: "id must be provided"}, errors.New("id must be provided")
	}
	thread, err := g.readThread(id)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return ToolResult{
		Success: true,
		Data:    thread,
		Text:    gmailThreadSummaryText(thread),
	}, nil
}

func (g *GmailCapability) executeListAttachments(params map[string]any) (ToolResult, error) {
	messageID := paramString(params, "message_id", "id")
	threadID := paramString(params, "thread_id")
	attachments, err := g.listAttachments(messageID, threadID)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return ToolResult{
		Success: true,
		Data:    attachments,
		Text:    gmailAttachmentListSummary(attachments),
	}, nil
}

func (g *GmailCapability) executeReadAttachment(params map[string]any) (ToolResult, error) {
	messageID := paramString(params, "message_id", "id")
	threadID := paramString(params, "thread_id")
	attachmentID := paramString(params, "attachment_id", "attachmentId")
	attachmentIDs := paramStringSlice(params, "attachment_ids", "attachmentIds", "ids")
	filename := paramString(params, "filename")
	filenames := paramStringSlice(params, "filenames", "names")
	readAll := paramBool(params, "read_all", "all")
	maxAttachments := paramInt(params, 6, "max_attachments", "max", "limit")
	if maxAttachments <= 0 {
		maxAttachments = 6
	}
	if attachmentID == "" && len(attachmentIDs) == 0 && filename == "" && len(filenames) == 0 {
		readAll = true
	}

	selections, err := g.selectAttachmentSelections(messageID, threadID, attachmentID, attachmentIDs, filename, filenames, readAll)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	truncated := false
	if maxAttachments > 0 && len(selections) > maxAttachments {
		selections = selections[:maxAttachments]
		truncated = true
	}
	indexed := make([]gmailIndexedAttachmentSelection, 0, len(selections))
	for i, selection := range selections {
		indexed = append(indexed, gmailIndexedAttachmentSelection{
			Index:     i + 1,
			Total:     len(selections),
			Selection: selection,
		})
	}

	results := make([]gmailAttachmentContentResult, 0, len(selections))
	readable := 0
	results = gmailParallelMap(indexed, gmailFetchConcurrency, func(item gmailIndexedAttachmentSelection) (gmailAttachmentContentResult, bool) {
		selection := item.Selection
		g.reportProgress(gmailAttachmentProgressLabel(item))
		entry := gmailAttachmentContentResult{
			MessageID:  selection.MessageID,
			ThreadID:   selection.ThreadID,
			Subject:    selection.Subject,
			From:       selection.From,
			Date:       selection.Date,
			Attachment: selection.Attachment,
		}
		data, err := g.downloadAttachmentData(selection.MessageID, selection.Attachment.AttachmentID)
		if err != nil {
			entry.Error = err.Error()
			g.reportProgress(gmailAttachmentFinishedLabel(item, err))
			return entry, true
		}
		content, err := g.readAttachmentContentSmart(data, selection.Attachment)
		if err != nil {
			entry.Error = err.Error()
			g.reportProgress(gmailAttachmentFinishedLabel(item, err))
			return entry, true
		}
		entry.Content = content
		entry.Preview = truncateForPrompt(content.Text, 600)
		entry.Readable = strings.TrimSpace(content.Text) != "" || len(content.Tables) > 0
		g.reportProgress(gmailAttachmentFinishedLabel(item, nil))
		return entry, true
	})
	for _, result := range results {
		if result.Readable {
			readable++
		}
	}

	text := gmailAttachmentReadSummary(results, truncated)
	return ToolResult{
		Success: true,
		Data: map[string]any{
			"attachments": results,
			"count":       len(results),
			"readable":    readable,
			"truncated":   truncated,
		},
		Text: text,
	}, nil
}

func (g *GmailCapability) reportProgress(line string) {
	g.mu.Lock()
	fn := g.ProgressFn
	g.mu.Unlock()
	if fn != nil {
		fn(line)
	}
}

func gmailAttachmentProgressLabel(item gmailIndexedAttachmentSelection) string {
	name := strings.TrimSpace(item.Selection.Attachment.Filename)
	if name == "" {
		name = strings.TrimSpace(item.Selection.Subject)
	}
	if name == "" {
		name = "attachment"
	}
	return fmt.Sprintf("reading attachment %d/%d: %s...", item.Index, item.Total, name)
}

func gmailAttachmentFinishedLabel(item gmailIndexedAttachmentSelection, err error) string {
	name := strings.TrimSpace(item.Selection.Attachment.Filename)
	if name == "" {
		name = strings.TrimSpace(item.Selection.Subject)
	}
	if name == "" {
		name = "attachment"
	}
	if err != nil {
		return fmt.Sprintf("finished attachment %d/%d: %s (error)", item.Index, item.Total, name)
	}
	return fmt.Sprintf("✓ finished attachment %d/%d: %s", item.Index, item.Total, name)
}

func (g *GmailCapability) readAttachmentContentSmart(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	content, err := ReadAttachmentContent(data, meta)
	if !gmailAttachmentNeedsOCRFallback(content, meta) && (strings.TrimSpace(content.Text) != "" || len(content.Tables) > 0) {
		if content.Metadata == nil {
			content.Metadata = map[string]string{}
		}
		if err != nil && content.Metadata["source"] == "" {
			content.Metadata["source"] = "primary reader returned partial content"
		}
		return content, nil
	}
	if ocrContent, ocrErr := gmailOCRAttachmentContent(data, meta); ocrErr == nil && (strings.TrimSpace(ocrContent.Text) != "" || len(ocrContent.Tables) > 0) {
		if content.Metadata == nil {
			content.Metadata = map[string]string{}
		}
		for k, v := range ocrContent.Metadata {
			content.Metadata[k] = v
		}
		content.Text = strings.TrimSpace(ocrContent.Text)
		content.Tables = append(content.Tables, ocrContent.Tables...)
		content.Warnings = append(content.Warnings, ocrContent.Warnings...)
		if len(content.Warnings) == 0 {
			content.Warnings = append(content.Warnings, "ocr fallback used")
		}
		return content, nil
	}
	if err != nil {
		return content, err
	}
	return content, nil
}

func gmailAttachmentNeedsOCRFallback(content AttachmentContent, meta AttachmentMeta) bool {
	text := strings.TrimSpace(content.Text)
	if text == "" && len(content.Tables) == 0 {
		return true
	}
	if !gmailAttachmentLooksLikeOCRCandidate(meta) {
		return false
	}
	if len(content.Tables) > 0 {
		return false
	}
	if strings.EqualFold(text, "Image attachment") {
		return true
	}
	if content.Metadata != nil && strings.EqualFold(content.Metadata["recovered_text"], "yes") {
		return false
	}
	for _, warning := range content.Warnings {
		lower := strings.ToLower(strings.TrimSpace(warning))
		if strings.Contains(lower, "no embedded text was recovered") || strings.Contains(lower, "best-effort only") {
			return true
		}
	}
	return false
}

func gmailOCRAttachmentContent(data []byte, meta AttachmentMeta) (AttachmentContent, error) {
	if !gmailAttachmentLooksLikeOCRCandidate(meta) {
		return AttachmentContent{}, errors.New("attachment is not an OCR candidate")
	}
	tempDir, err := os.MkdirTemp("", "jot-ocr-*")
	if err != nil {
		return AttachmentContent{}, err
	}
	defer os.RemoveAll(tempDir)
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(meta.Filename)))
	if ext == "" {
		ext = ".bin"
	}
	inputPath := filepath.Join(tempDir, "input"+ext)
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return AttachmentContent{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	ocrText, ocrWarnings, ocrMeta, ocrErr := gmailRunBestAvailableOCR(ctx, inputPath, tempDir)
	if ocrErr == nil && strings.TrimSpace(ocrText) != "" {
		return AttachmentContent{
			Text:     strings.TrimSpace(ocrText),
			Metadata: ocrMeta,
			Warnings: append([]string(nil), ocrWarnings...),
		}, nil
	}
	if !gmailAttachmentLooksLikePdf(meta) {
		if ocrErr != nil {
			return AttachmentContent{}, ocrErr
		}
		return AttachmentContent{}, errors.New("ocr returned no text")
	}
	if convertedPath, convWarnings, convErr := gmailPreparePdfForOCR(ctx, inputPath, tempDir); convErr == nil && convertedPath != "" {
		ocrText, ocrWarnings, ocrMeta, ocrErr = gmailRunBestAvailableOCR(ctx, convertedPath, tempDir)
		if ocrErr == nil && strings.TrimSpace(ocrText) != "" {
			warnings := append([]string(nil), convWarnings...)
			warnings = append(warnings, ocrWarnings...)
			metaMap := copyStringMap(ocrMeta)
			if metaMap == nil {
				metaMap = map[string]string{}
			}
			metaMap["mode"] = "pdf-converted"
			return AttachmentContent{
				Text:     strings.TrimSpace(ocrText),
				Metadata: metaMap,
				Warnings: warnings,
			}, nil
		}
		if ocrErr == nil {
			ocrErr = errors.New("ocr returned no text")
		}
		if len(convWarnings) > 0 {
			ocrWarnings = append(convWarnings, ocrWarnings...)
		}
	}
	if ocrErr != nil {
		return AttachmentContent{}, ocrErr
	}
	return AttachmentContent{}, errors.New("ocr returned no text")
}

func gmailRunBestAvailableOCR(ctx context.Context, inputPath, tempDir string) (string, []string, map[string]string, error) {
	var errs []string
	if tesseractPath, err := exec.LookPath("tesseract"); err == nil {
		if text, warnings, ocrErr := gmailRunTesseractOCR(ctx, tesseractPath, inputPath, tempDir); ocrErr == nil && strings.TrimSpace(text) != "" {
			return text, warnings, map[string]string{
				"type": "ocr/tesseract",
				"tool": "tesseract",
				"mode": "direct",
			}, nil
		} else if ocrErr != nil {
			errs = append(errs, ocrErr.Error())
		}
	} else if strings.TrimSpace(err.Error()) != "" {
		errs = append(errs, err.Error())
	}
	if text, warnings, ocrErr := gmailRunWindowsOCR(ctx, inputPath, tempDir); ocrErr == nil && strings.TrimSpace(text) != "" {
		return text, warnings, map[string]string{
			"type": "ocr/windows",
			"tool": "windows-ocr",
			"mode": "direct",
		}, nil
	} else if ocrErr != nil {
		errs = append(errs, ocrErr.Error())
	}
	if len(errs) == 0 {
		errs = append(errs, "no OCR engine produced text")
	}
	return "", nil, nil, errors.New(strings.Join(uniqueTrimmedStrings(errs), "; "))
}

func gmailAttachmentLooksLikeOCRCandidate(meta AttachmentMeta) bool {
	if gmailAttachmentLooksLikeImage(meta) {
		return true
	}
	if gmailAttachmentLooksLikePdf(meta) {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	for _, token := range []string{"scan", "photo", "image", "screenshot", "passport", "id", "visa", "permit"} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func gmailAttachmentLooksLikePdf(meta AttachmentMeta) bool {
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	mime := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if strings.Contains(mime, "pdf") {
		return true
	}
	return strings.HasSuffix(name, ".pdf")
}

func gmailRunTesseractOCR(ctx context.Context, tesseractPath, inputPath, tempDir string) (string, []string, error) {
	outputBase := filepath.Join(tempDir, "ocr-output")
	cmd := exec.CommandContext(ctx, tesseractPath, inputPath, outputBase, "--psm", "6")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("tesseract OCR failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	text, err := os.ReadFile(outputBase + ".txt")
	if err != nil {
		return "", nil, err
	}
	return string(text), []string{"ocr fallback used"}, nil
}

func gmailRunWindowsOCR(ctx context.Context, inputPath, tempDir string) (string, []string, error) {
	if runtime.GOOS != "windows" {
		return "", nil, errors.New("windows ocr is only available on windows")
	}
	powershellPath, err := exec.LookPath("powershell")
	if err != nil {
		return "", nil, err
	}
	scriptPath := filepath.Join(tempDir, "windows-ocr.ps1")
	outputPath := filepath.Join(tempDir, "windows-ocr.txt")
	if err := os.WriteFile(scriptPath, []byte(gmailWindowsOCRScript()), 0o600); err != nil {
		return "", nil, err
	}
	cmd := exec.CommandContext(ctx, powershellPath,
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath,
		"-ImagePath", inputPath,
		"-OutputPath", outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, fmt.Errorf("windows OCR failed: %w: %s", err, strings.TrimSpace(string(output)))
	}
	text, err := os.ReadFile(outputPath)
	if err != nil {
		return "", nil, err
	}
	return string(text), []string{"ocr fallback used", "windows ocr used"}, nil
}

func gmailWindowsOCRScript() string {
	return strings.TrimSpace(`
param(
  [Parameter(Mandatory = $true)][string]$ImagePath,
  [Parameter(Mandatory = $true)][string]$OutputPath
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime | Out-Null
[void][Windows.Storage.StorageFile, Windows.Storage, ContentType=WindowsRuntime]
[void][Windows.Storage.FileAccessMode, Windows.Storage, ContentType=WindowsRuntime]
[void][Windows.Storage.Streams.IRandomAccessStream, Windows.Storage.Streams, ContentType=WindowsRuntime]
[void][Windows.Graphics.Imaging.BitmapDecoder, Windows.Foundation, ContentType=WindowsRuntime]
[void][Windows.Graphics.Imaging.SoftwareBitmap, Windows.Foundation, ContentType=WindowsRuntime]
[void][Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType=WindowsRuntime]
[void][Windows.Media.Ocr.OcrResult, Windows.Foundation, ContentType=WindowsRuntime]
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8

function AwaitWinRT([object]$Operation, [type]$ResultType) {
  $Method = [System.WindowsRuntimeSystemExtensions].GetMethods() |
    Where-Object {
      $_.Name -eq 'AsTask' -and
      $_.IsGenericMethodDefinition -and
      $_.GetGenericArguments().Count -eq 1 -and
      $_.GetParameters().Count -eq 1
    } |
    Select-Object -First 1
  $Generic = $Method.MakeGenericMethod($ResultType)
  $Task = $Generic.Invoke($null, @($Operation))
  return $Task.GetAwaiter().GetResult()
}

$File = AwaitWinRT ([Windows.Storage.StorageFile]::GetFileFromPathAsync($ImagePath)) ([Windows.Storage.StorageFile])
$Stream = AwaitWinRT ($File.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
$Decoder = AwaitWinRT ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($Stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
$Bitmap = AwaitWinRT ($Decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
$Engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if ($null -eq $Engine) {
  throw 'Windows OCR engine is unavailable for the current user profile languages.'
}
$Result = AwaitWinRT ($Engine.RecognizeAsync($Bitmap)) ([Windows.Media.Ocr.OcrResult])
[System.IO.File]::WriteAllText($OutputPath, $Result.Text, [System.Text.Encoding]::UTF8)
`)
}

func copyStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func gmailPreparePdfForOCR(ctx context.Context, inputPath, tempDir string) (string, []string, error) {
	type pdfConverter struct {
		name       string
		args       func(inputPath, outputBase string) []string
		outputPath func(outputBase string) string
	}
	converters := []pdfConverter{
		{
			name: "pdftoppm",
			args: func(inputPath, outputBase string) []string {
				return []string{"-png", "-f", "1", "-singlefile", inputPath, outputBase}
			},
			outputPath: func(outputBase string) string { return outputBase + ".png" },
		},
		{
			name: "mutool",
			args: func(inputPath, outputBase string) []string {
				return []string{"draw", "-F", "png", "-o", outputBase + ".png", inputPath, "1"}
			},
			outputPath: func(outputBase string) string { return outputBase + ".png" },
		},
	}
	for _, converter := range converters {
		bin, err := exec.LookPath(converter.name)
		if err != nil {
			continue
		}
		outputBase := filepath.Join(tempDir, "pdf-page")
		cmd := exec.CommandContext(ctx, bin, converter.args(inputPath, outputBase)...)
		if _, err := cmd.CombinedOutput(); err == nil {
			path := converter.outputPath(outputBase)
			if _, statErr := os.Stat(path); statErr == nil {
				return path, []string{converter.name + " used for pdf ocr"}, nil
			}
		}
	}
	return "", nil, errors.New("no pdf converter available for OCR")
}

func (g *GmailCapability) executeDownloadAttachment(params map[string]any) (ToolResult, error) {
	messageID := paramString(params, "message_id", "id")
	threadID := paramString(params, "thread_id")
	attachmentID := paramString(params, "attachment_id", "attachmentId")
	attachmentIDs := paramStringSlice(params, "attachment_ids", "attachmentIds", "ids")
	filename := paramString(params, "filename")
	filenames := paramStringSlice(params, "filenames", "names")
	downloadAll := paramBool(params, "download_all", "all")
	saveDir := strings.TrimSpace(paramString(params, "save_dir", "dir", "path"))
	if saveDir == "" {
		saveDir = strings.TrimSpace(g.AttachmentSaveDir)
	}
	if saveDir == "" {
		saveDir = "."
	}

	selections, err := g.selectAttachmentDownloads(messageID, threadID, attachmentID, attachmentIDs, filename, filenames, downloadAll)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	results := make([]gmailAttachmentDownloadFile, 0, len(selections))
	var totalBytes int64
	var first gmailAttachmentDownloadFile
	for i, selection := range selections {
		data, err := g.downloadAttachmentData(selection.MessageID, selection.Attachment.AttachmentID)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		attachment := selection.Attachment
		targetDir := saveDir
		if strings.TrimSpace(targetDir) == "" {
			targetDir = "."
		}
		savePath, err := gmailResolveAttachmentSavePath(targetDir, attachment.Filename, attachment.AttachmentID)
		if err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if err := os.MkdirAll(filepath.Dir(savePath), 0o755); err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		if err := os.WriteFile(savePath, data, 0o600); err != nil {
			return ToolResult{Success: false, Error: err.Error()}, err
		}
		file := gmailAttachmentDownloadFile{
			MessageID:    selection.MessageID,
			ThreadID:     selection.ThreadID,
			Subject:      selection.Subject,
			From:         selection.From,
			Date:         selection.Date,
			Filename:     attachment.Filename,
			MimeType:     attachment.MimeType,
			AttachmentID: attachment.AttachmentID,
			SavedPath:    savePath,
			Bytes:        int64(len(data)),
		}
		if i == 0 {
			first = file
		}
		results = append(results, file)
		totalBytes += file.Bytes
	}

	return ToolResult{
		Success: true,
		Data: gmailAttachmentDownloadResult{
			SavedPath: first.SavedPath,
			Filename:  first.Filename,
			Bytes:     totalBytes,
			Count:     len(results),
			MessageID: first.MessageID,
			ThreadID:  first.ThreadID,
			Subject:   first.Subject,
			From:      first.From,
			Date:      first.Date,
			Files:     results,
		},
		Text: gmailAttachmentDownloadSummary(results),
	}, nil
}

func (g *GmailCapability) executeArchiveThread(params map[string]any) (ToolResult, error) {
	target, err := g.resolveLabelMutationTarget(params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if err := g.applyLabelMutation(target, nil, []string{"INBOX"}); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return g.labelMutationResult("archive_thread", target, nil, []string{"INBOX"}), nil
}

func (g *GmailCapability) executeMarkRead(params map[string]any) (ToolResult, error) {
	target, err := g.resolveLabelMutationTarget(params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if err := g.applyLabelMutation(target, nil, []string{"UNREAD"}); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return g.labelMutationResult("mark_read", target, nil, []string{"UNREAD"}), nil
}

func (g *GmailCapability) executeStarThread(params map[string]any) (ToolResult, error) {
	target, err := g.resolveLabelMutationTarget(params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if err := g.applyLabelMutation(target, []string{"STARRED"}, nil); err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	return g.labelMutationResult("star_thread", target, []string{"STARRED"}, nil), nil
}

func (g *GmailCapability) executeExtractActions(params map[string]any) (ToolResult, error) {
	text := strings.TrimSpace(paramString(params, "text", "body", "message"))
	if text == "" {
		messageID := paramString(params, "message_id", "id")
		if messageID != "" {
			msg, err := g.readMessage(messageID)
			if err != nil {
				return ToolResult{Success: false, Error: err.Error()}, err
			}
			text = msg.BodyText
			if text == "" {
				text = msg.Snippet
			}
		}
	}
	if strings.TrimSpace(text) == "" {
		return ToolResult{Success: false, Error: "text or message_id must be provided"}, errors.New("text or message_id must be provided")
	}
	actions := g.inferActions(text, time.Now())
	return ToolResult{Success: true, Data: actions, Text: gmailActionSummary(actions)}, nil
}

func (g *GmailCapability) executeDraftReply(params map[string]any) (ToolResult, error) {
	messageID := paramString(params, "message_id", "id")
	threadID := paramString(params, "thread_id")
	body := strings.TrimSpace(paramString(params, "body", "reply"))
	msg, thread, err := g.resolveReplyTarget(messageID, threadID)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	if body == "" {
		return ToolResult{Success: false, Error: "body must be provided"}, errors.New("body must be provided")
	}
	sendRequested := paramBool(params, "send", "deliver")
	subject := gmailHeaderValue(msg.Payload.Headers, "Subject")
	if subject == "" {
		subject = "Re:"
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(subject)), "re:") {
		subject = "Re: " + strings.TrimSpace(subject)
	}
	replyRaw, err := gmailComposeReplyRaw(msg, body, subject)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}

	replyTo := gmailHeaderValue(msg.Payload.Headers, "Reply-To")
	if strings.TrimSpace(replyTo) == "" {
		replyTo = gmailHeaderValue(msg.Payload.Headers, "From")
	}

	data := map[string]any{
		"preview":          body,
		"message_id":       msg.ID,
		"thread_id":        msg.ThreadID,
		"reply_to":         replyTo,
		"subject":          subject,
		"body":             body,
		"send_requested":   sendRequested,
		"send_allowed":     g.sendScopeAvailable(),
		"confirmation_req": sendRequested && !g.sendScopeAvailable(),
		"original_subject": gmailHeaderValue(msg.Payload.Headers, "Subject"),
		"original_from":    gmailHeaderValue(msg.Payload.Headers, "From"),
		"original_snippet": strings.TrimSpace(msg.Snippet),
	}
	if thread != nil {
		data["thread"] = thread
	}

	if sendRequested {
		sent, err := g.sendRawMessage(replyRaw)
		if err != nil {
			sendErr := gmailNormalizeSendPermissionError(err)
			draft, draftErr := g.createDraft(replyRaw)
			if draftErr != nil {
				draftErr = gmailNormalizeSendPermissionError(draftErr)
				return ToolResult{Success: false, Error: fmt.Sprintf("%s; also failed to save draft: %s", sendErr.Error(), draftErr.Error())}, fmt.Errorf("%s; also failed to save draft: %s", sendErr.Error(), draftErr.Error())
			}
			data["draft"] = draft
			data["sent"] = false
			data["draft_fallback"] = true
			data["send_error"] = sendErr.Error()
			return ToolResult{
				Success: true,
				Data:    data,
				Text:    fmt.Sprintf("couldn't send the reply, so I saved it as a draft for %s", replyTo),
			}, nil
		}
		data["sent"] = true
		data["send_result"] = sent
		return ToolResult{Success: true, Data: data, Text: fmt.Sprintf("reply sent to %s", replyTo)}, nil
	}

	draft, err := g.createDraft(replyRaw)
	if err != nil {
		err = gmailNormalizeSendPermissionError(err)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	data["draft"] = draft
	if sendRequested && !g.sendScopeAvailable() {
		data["requires_confirmation"] = true
		data["sent"] = false
	}
	return ToolResult{Success: true, Data: data, Text: fmt.Sprintf("draft prepared for %s", replyTo)}, nil
}

func (g *GmailCapability) executeSendEmail(params map[string]any) (ToolResult, error) {
	to := strings.TrimSpace(paramString(params, "to", "reply_to"))
	subject := strings.TrimSpace(paramString(params, "subject"))
	body := strings.TrimSpace(paramString(params, "body", "message"))
	if to == "" {
		return ToolResult{Success: false, Error: "to must be provided"}, errors.New("to must be provided")
	}
	if subject == "" {
		return ToolResult{Success: false, Error: "subject must be provided"}, errors.New("subject must be provided")
	}
	if body == "" {
		return ToolResult{Success: false, Error: "body must be provided"}, errors.New("body must be provided")
	}
	attachmentPaths, err := resolveAttachmentSendPaths(params)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	raw, err := gmailComposeRaw(to, subject, body, attachmentPaths)
	if err != nil {
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	sendRequested := paramBool(params, "send", "deliver")
	data := map[string]any{
		"preview":          body,
		"body":             body,
		"to":               to,
		"subject":          subject,
		"send_requested":   sendRequested,
		"send_allowed":     g.sendScopeAvailable(),
		"channel":          "gmail",
		"attachment_count": len(attachmentPaths),
		"attachment_paths": attachmentPaths,
	}
	if sendRequested {
		sent, err := g.sendRawMessage(raw)
		if err != nil {
			sendErr := gmailNormalizeSendPermissionError(err)
			draft, draftErr := g.createDraft(raw)
			if draftErr != nil {
				draftErr = gmailNormalizeSendPermissionError(draftErr)
				return ToolResult{Success: false, Error: fmt.Sprintf("%s; also failed to save draft: %s", sendErr.Error(), draftErr.Error())}, fmt.Errorf("%s; also failed to save draft: %s", sendErr.Error(), draftErr.Error())
			}
			data["draft"] = draft
			data["sent"] = false
			data["draft_fallback"] = true
			data["send_error"] = sendErr.Error()
			return ToolResult{
				Success: true,
				Data:    data,
				Text:    fmt.Sprintf("couldn't send the email, so I saved it as a draft for %s", to),
			}, nil
		}
		data["sent"] = true
		data["send_result"] = sent
		return ToolResult{Success: true, Data: data, Text: fmt.Sprintf("email sent to %s", to)}, nil
	}
	draft, err := g.createDraft(raw)
	if err != nil {
		err = gmailNormalizeSendPermissionError(err)
		return ToolResult{Success: false, Error: err.Error()}, err
	}
	data["draft"] = draft
	return ToolResult{Success: true, Data: data, Text: fmt.Sprintf("draft prepared for %s", to)}, nil
}

type gmailAttachmentSelection struct {
	MessageID     string
	ThreadID      string
	Subject       string
	From          string
	Date          time.Time
	Attachment    AttachmentMeta
	MessageIndex  int
	AttachmentIdx int
}

func (g *GmailCapability) listAttachments(messageID, threadID string) ([]NormalizedEmail, error) {
	selections, err := g.selectAttachmentSelections(messageID, threadID, "", nil, "", nil, false)
	if err != nil {
		return nil, err
	}
	results := make([]NormalizedEmail, 0, len(selections))
	for _, selection := range selections {
		results = append(results, gmailAttachmentSelectionToEmail(selection))
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Date.Equal(results[j].Date) {
			return results[i].Subject < results[j].Subject
		}
		return results[i].Date.After(results[j].Date)
	})
	return results, nil
}

func (g *GmailCapability) selectAttachmentDownloads(messageID, threadID, attachmentID string, attachmentIDs []string, filename string, filenames []string, downloadAll bool) ([]gmailAttachmentSelection, error) {
	return g.selectAttachmentSelections(messageID, threadID, attachmentID, attachmentIDs, filename, filenames, downloadAll)
}

func (g *GmailCapability) selectAttachmentSelections(messageID, threadID, attachmentID string, attachmentIDs []string, filename string, filenames []string, downloadAll bool) ([]gmailAttachmentSelection, error) {
	sources, err := g.collectAttachmentSources(messageID, threadID)
	if err != nil {
		return nil, err
	}
	var selections []gmailAttachmentSelection
	wantAll := downloadAll || attachmentID == "all" || strings.EqualFold(filename, "all")
	if len(attachmentIDs) > 0 {
		wantAll = wantAll || containsString(attachmentIDs, "all")
	}
	if len(filenames) > 0 {
		for _, name := range filenames {
			if strings.EqualFold(strings.TrimSpace(name), "all") {
				wantAll = true
				break
			}
		}
	}
	for sourceIdx, source := range sources {
		for attIdx, att := range source.Attachments {
			selection := gmailAttachmentSelection{
				MessageID:     source.Email.ID,
				ThreadID:      source.Email.ThreadID,
				Subject:       source.Email.Subject,
				From:          source.Email.From,
				Date:          source.Email.Date,
				Attachment:    att,
				MessageIndex:  sourceIdx,
				AttachmentIdx: attIdx,
			}
			if wantAll {
				selections = append(selections, selection)
				continue
			}
			if attachmentID != "" && strings.EqualFold(att.AttachmentID, attachmentID) {
				selections = append(selections, selection)
				continue
			}
			if filename != "" && gmailAttachmentNameMatches(att, filename) {
				selections = append(selections, selection)
				continue
			}
			if len(attachmentIDs) > 0 && containsString(attachmentIDs, att.AttachmentID) {
				selections = append(selections, selection)
				continue
			}
			if len(filenames) > 0 {
				for _, candidate := range filenames {
					if gmailAttachmentNameMatches(att, candidate) {
						selections = append(selections, selection)
						break
					}
				}
			}
		}
	}
	sort.SliceStable(selections, func(i, j int) bool {
		pi := gmailAttachmentSelectionPriority(selections[i])
		pj := gmailAttachmentSelectionPriority(selections[j])
		if pi != pj {
			return pi > pj
		}
		if selections[i].Date.Equal(selections[j].Date) {
			return selections[i].AttachmentIdx < selections[j].AttachmentIdx
		}
		if selections[i].Date.IsZero() {
			return false
		}
		if selections[j].Date.IsZero() {
			return true
		}
		return selections[i].Date.After(selections[j].Date)
	})

	if wantAll {
		if len(selections) == 0 {
			return nil, errors.New("no attachments found to download")
		}
		return selections, nil
	}
	if len(selections) == 0 && attachmentID == "" && filename == "" && len(attachmentIDs) == 0 && len(filenames) == 0 {
		if len(sources) == 1 && len(sources[0].Attachments) == 1 {
			return []gmailAttachmentSelection{{
				MessageID:     sources[0].Email.ID,
				ThreadID:      sources[0].Email.ThreadID,
				Subject:       sources[0].Email.Subject,
				From:          sources[0].Email.From,
				Date:          sources[0].Email.Date,
				Attachment:    sources[0].Attachments[0],
				MessageIndex:  0,
				AttachmentIdx: 0,
			}}, nil
		}
		return nil, errors.New("attachment_id, filename, or download_all must be provided")
	}
	if len(selections) == 0 {
		return nil, errors.New("no matching attachments found")
	}
	return selections, nil
}

type gmailAttachmentSource struct {
	Email       NormalizedEmail
	Attachments []AttachmentMeta
}

func (g *GmailCapability) collectAttachmentSources(messageID, threadID string) ([]gmailAttachmentSource, error) {
	switch {
	case strings.TrimSpace(threadID) != "":
		thread, err := g.readThread(threadID)
		if err != nil {
			return nil, err
		}
		sources := make([]gmailAttachmentSource, 0, len(thread.Messages))
		for _, email := range thread.Messages {
			if len(email.Attachments) == 0 {
				continue
			}
			sources = append(sources, gmailAttachmentSource{Email: email, Attachments: append([]AttachmentMeta(nil), email.Attachments...)})
		}
		sort.SliceStable(sources, func(i, j int) bool {
			return gmailAttachmentSourcePriority(sources[i]) > gmailAttachmentSourcePriority(sources[j])
		})
		return sources, nil
	case strings.TrimSpace(messageID) != "":
		msg, err := g.readMessage(messageID)
		if err != nil {
			return nil, err
		}
		if len(msg.Attachments) == 0 {
			return []gmailAttachmentSource{{Email: msg, Attachments: nil}}, nil
		}
		return []gmailAttachmentSource{{Email: msg, Attachments: append([]AttachmentMeta(nil), msg.Attachments...)}}, nil
	default:
		return nil, errors.New("message_id or thread_id must be provided")
	}
}

func gmailAttachmentSelectionToEmail(selection gmailAttachmentSelection) NormalizedEmail {
	filename := selection.Attachment.Filename
	if filename == "" {
		filename = selection.Subject
	}
	detailParts := []string{}
	if selection.Subject != "" {
		detailParts = append(detailParts, selection.Subject)
	}
	if selection.Attachment.MimeType != "" {
		detailParts = append(detailParts, selection.Attachment.MimeType)
	}
	if selection.Attachment.SizeBytes > 0 {
		detailParts = append(detailParts, assistantFormatKB(selection.Attachment.SizeBytes))
	}
	if selection.Attachment.AttachmentID != "" {
		detailParts = append(detailParts, "id:"+selection.Attachment.AttachmentID)
	}
	detail := strings.TrimSpace(strings.Join(detailParts, " · "))
	if detail == "" {
		detail = selection.Attachment.Filename
	}
	return NormalizedEmail{
		ID:       attachmentSelectionID(selection),
		ThreadID: selection.ThreadID,
		Subject:  filename,
		From:     selection.From,
		Date:     selection.Date,
		Snippet:  detail,
		Attachments: []AttachmentMeta{
			selection.Attachment,
		},
	}
}

func attachmentSelectionID(selection gmailAttachmentSelection) string {
	if selection.Attachment.AttachmentID != "" {
		return selection.Attachment.AttachmentID
	}
	return fmt.Sprintf("%s:%s", selection.MessageID, selection.Attachment.Filename)
}

func gmailAttachmentNameMatches(meta AttachmentMeta, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return false
	}
	if strings.EqualFold(meta.Filename, candidate) {
		return true
	}
	return strings.EqualFold(filepath.Base(meta.Filename), filepath.Base(candidate))
}

func gmailThreadSummaryText(thread gmailThreadResult) string {
	count := len(thread.Messages)
	if count == 0 {
		return fmt.Sprintf("thread %s has no messages", thread.ThreadID)
	}
	subject := strings.TrimSpace(thread.Subject)
	if subject == "" {
		subject = strings.TrimSpace(thread.Messages[0].Subject)
	}
	if subject == "" {
		subject = "thread"
	}
	parts := []string{fmt.Sprintf("thread %s", thread.ThreadID)}
	if subject != "" {
		parts = append(parts, subject)
	}
	parts = append(parts, fmt.Sprintf("%d message(s)", count))
	if thread.AttachmentCount > 0 {
		parts = append(parts, fmt.Sprintf("%d attachment(s)", thread.AttachmentCount))
	}
	if len(thread.Participants) > 0 {
		parts = append(parts, strings.Join(thread.Participants, ", "))
	}
	return strings.Join(parts, " · ")
}

func gmailAttachmentListSummary(emails []NormalizedEmail) string {
	if len(emails) == 0 {
		return "no attachments found"
	}
	var parts []string
	for i, email := range emails {
		if i >= 6 {
			parts = append(parts, fmt.Sprintf("and %d more", len(emails)-i))
			break
		}
		label := email.Subject
		if label == "" && len(email.Attachments) > 0 {
			label = email.Attachments[0].Filename
		}
		suffix := ""
		if email.Snippet != "" {
			suffix = " - " + email.Snippet
		}
		parts = append(parts, fmt.Sprintf("%s%s", label, suffix))
	}
	return fmt.Sprintf("%d attachment(s): %s", len(emails), strings.Join(parts, "; "))
}

func gmailAttachmentReadSummary(results []gmailAttachmentContentResult, truncated bool) string {
	if len(results) == 0 {
		return "no attachments read"
	}
	readable := 0
	unreadable := 0
	var names []string
	for i, result := range results {
		if i < 5 {
			names = append(names, result.Attachment.Filename)
		}
		if result.Readable {
			readable++
		} else {
			unreadable++
		}
	}
	summary := fmt.Sprintf("read %d attachment(s)", len(results))
	if readable > 0 {
		summary += fmt.Sprintf(", %d extracted", readable)
	}
	if unreadable > 0 {
		summary += fmt.Sprintf(", %d unsupported or unreadable", unreadable)
	}
	if len(names) > 0 {
		summary += ": " + strings.Join(names, ", ")
	}
	if truncated {
		summary += " (truncated)"
	}
	return summary
}

func gmailAttachmentDownloadSummary(files []gmailAttachmentDownloadFile) string {
	if len(files) == 0 {
		return "no attachments saved"
	}
	if len(files) == 1 {
		return fmt.Sprintf("saved %s", files[0].SavedPath)
	}
	dir := filepath.Dir(files[0].SavedPath)
	return fmt.Sprintf("saved %d attachment(s) to %s", len(files), dir)
}

func (g *GmailCapability) resolveReplyTarget(messageID, threadID string) (*gmailMessage, *gmailThreadResult, error) {
	if strings.TrimSpace(messageID) != "" {
		msg, err := g.readRawMessage(messageID)
		if err != nil {
			return nil, nil, err
		}
		return msg, nil, nil
	}
	if strings.TrimSpace(threadID) == "" {
		return nil, nil, errors.New("message_id or thread_id must be provided")
	}
	thread, err := g.readThread(threadID)
	if err != nil {
		return nil, nil, err
	}
	if len(thread.Messages) == 0 {
		return nil, nil, fmt.Errorf("thread %q does not contain any messages", threadID)
	}
	msg, err := g.readRawMessage(thread.Messages[0].ID)
	if err != nil {
		return nil, nil, err
	}
	return msg, &thread, nil
}

func (g *GmailCapability) resolveLabelMutationTarget(params map[string]any) (gmailLabelMutationTarget, error) {
	threadID := paramString(params, "thread_id", "threadId")
	messageID := paramString(params, "message_id", "messageId")
	id := paramString(params, "id")
	if threadID == "" && messageID == "" && id != "" {
		threadID = id
		messageID = id
	}

	if threadID != "" {
		thread, err := g.readThread(threadID)
		if err == nil {
			return gmailLabelMutationTargetFromThread(thread), nil
		}
		if messageID == "" {
			messageID = threadID
		}
		if msg, fallbackErr := g.readRawMessage(messageID); fallbackErr == nil {
			if msg.ThreadID != "" {
				if thread, threadErr := g.readThread(msg.ThreadID); threadErr == nil {
					return gmailLabelMutationTargetFromThread(thread), nil
				}
			}
			return gmailLabelMutationTargetFromMessage(*msg), nil
		}
		return gmailLabelMutationTarget{}, err
	}

	if messageID != "" {
		msg, err := g.readRawMessage(messageID)
		if err != nil {
			return gmailLabelMutationTarget{}, err
		}
		if msg.ThreadID != "" {
			if thread, threadErr := g.readThread(msg.ThreadID); threadErr == nil {
				return gmailLabelMutationTargetFromThread(thread), nil
			}
		}
		return gmailLabelMutationTargetFromMessage(*msg), nil
	}

	return gmailLabelMutationTarget{}, errors.New("thread_id or message_id must be provided")
}

func (g *GmailCapability) applyLabelMutation(target gmailLabelMutationTarget, add, remove []string) error {
	req := gmailLabelMutationRequest{
		AddLabelIDs:    cloneAndTrimStrings(add),
		RemoveLabelIDs: cloneAndTrimStrings(remove),
	}
	if target.Kind == "thread" && strings.TrimSpace(target.ThreadID) != "" {
		if err := g.postJSON("/gmail/v1/users/me/threads/"+url.PathEscape(target.ThreadID)+"/modify", req, &struct{}{}); err == nil {
			return nil
		} else if strings.TrimSpace(target.MessageID) == "" {
			return err
		}
	}
	if strings.TrimSpace(target.MessageID) == "" {
		if target.Kind == "thread" {
			return fmt.Errorf("thread %q has no fallback message to modify", target.ThreadID)
		}
		return errors.New("message_id must be available to modify labels")
	}
	if err := g.postJSON("/gmail/v1/users/me/messages/"+url.PathEscape(target.MessageID)+"/modify", req, &struct{}{}); err != nil {
		return err
	}
	return nil
}

func gmailLabelMutationTargetFromThread(thread gmailThreadResult) gmailLabelMutationTarget {
	target := gmailLabelMutationTarget{
		Kind:         "thread",
		ThreadID:     thread.ThreadID,
		Subject:      strings.TrimSpace(thread.Subject),
		MessageCount: thread.MessageCount,
		Participants: append([]string(nil), thread.Participants...),
		Date:         thread.LatestDate,
	}
	if len(thread.Messages) > 0 {
		target.MessageID = thread.Messages[0].ID
		if target.Subject == "" {
			target.Subject = strings.TrimSpace(thread.Messages[0].Subject)
		}
		if target.From == "" {
			target.From = strings.TrimSpace(thread.Messages[0].From)
		}
		if target.Date.IsZero() {
			target.Date = thread.Messages[0].Date
		}
		for _, message := range thread.Messages {
			if message.Unread {
				target.Unread = true
				break
			}
		}
	}
	return target
}

func gmailLabelMutationTargetFromMessage(msg gmailMessage) gmailLabelMutationTarget {
	norm := gmailNormalizeMessage(msg)
	return gmailLabelMutationTarget{
		Kind:      "message",
		ThreadID:  norm.ThreadID,
		MessageID: norm.ID,
		Subject:   norm.Subject,
		From:      norm.From,
		Date:      norm.Date,
		Unread:    norm.Unread,
	}
}

func (g *GmailCapability) labelMutationResult(operation string, target gmailLabelMutationTarget, add, remove []string) ToolResult {
	add = cloneAndTrimStrings(add)
	remove = cloneAndTrimStrings(remove)
	sort.Strings(add)
	sort.Strings(remove)
	text := gmailLabelMutationText(operation, target, add, remove)
	data := map[string]any{
		"operation":     operation,
		"target":        target,
		"addedLabels":   add,
		"removedLabels": remove,
	}
	return ToolResult{Success: true, Data: data, Text: text}
}

func gmailLabelMutationText(operation string, target gmailLabelMutationTarget, add, remove []string) string {
	label := gmailLabelMutationTargetLabel(target)
	switch operation {
	case "archive_thread":
		if label != "" {
			return fmt.Sprintf("archived %s", label)
		}
		return "archived email"
	case "mark_read":
		if label != "" {
			return fmt.Sprintf("marked %s read", label)
		}
		return "marked email read"
	case "star_thread":
		if label != "" {
			return fmt.Sprintf("starred %s", label)
		}
		return "starred email"
	default:
		parts := []string{operation}
		if label != "" {
			parts = append(parts, label)
		}
		if len(add) > 0 {
			parts = append(parts, "added: "+strings.Join(add, ", "))
		}
		if len(remove) > 0 {
			parts = append(parts, "removed: "+strings.Join(remove, ", "))
		}
		return strings.Join(parts, " · ")
	}
}

func gmailLabelMutationTargetLabel(target gmailLabelMutationTarget) string {
	subject := strings.TrimSpace(target.Subject)
	from := strings.TrimSpace(target.From)
	switch {
	case subject != "" && from != "":
		return fmt.Sprintf("%s from %s", subject, from)
	case subject != "":
		return subject
	case from != "":
		return from
	case target.ThreadID != "":
		return fmt.Sprintf("thread %s", target.ThreadID)
	case target.MessageID != "":
		return fmt.Sprintf("message %s", target.MessageID)
	default:
		return ""
	}
}

func gmailResolveTokenPath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	dir, err := gmailConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gmail_token.json"), nil
}

func gmailResolveCredentialPath(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	dir, err := gmailConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gmail_credentials.json"), nil
}

func gmailConfigDir() (string, error) {
	if dir, err := os.UserConfigDir(); err == nil && dir != "" {
		return filepath.Join(dir, "jot"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "jot"), nil
}

func gmailLoadOAuthCredentials(path string) (*gmailOAuthCredentials, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		data = trimUTF8BOM(data)
		var creds gmailOAuthCredentials
		if err := json.Unmarshal(data, &creds); err != nil {
			var wrapped struct {
				Installed gmailOAuthCredentials `json:"installed"`
				Web       gmailOAuthCredentials `json:"web"`
			}
			if err := json.Unmarshal(data, &wrapped); err != nil {
				return nil, err
			}
			switch {
			case strings.TrimSpace(wrapped.Installed.ClientID) != "":
				creds = wrapped.Installed
			case strings.TrimSpace(wrapped.Web.ClientID) != "":
				creds = wrapped.Web
			default:
				return nil, os.ErrNotExist
			}
		}
		if creds.ClientID != "" {
			if creds.TokenURL == "" {
				creds.TokenURL = gmailTokenURL
			}
			if len(creds.Scopes) == 0 {
				creds.Scopes = append([]string(nil), gmailRequiredScopes...)
			}
			return &creds, nil
		}
	}

	clientID := strings.TrimSpace(os.Getenv("JOT_GMAIL_CLIENT_ID"))
	if clientID == "" {
		return nil, os.ErrNotExist
	}
	creds := &gmailOAuthCredentials{
		ClientID:     clientID,
		ClientSecret: strings.TrimSpace(os.Getenv("JOT_GMAIL_CLIENT_SECRET")),
		DeviceURL:    gmailDeviceCodeURL,
		TokenURL:     gmailTokenURL,
		Scopes:       append([]string(nil), gmailRequiredScopes...),
	}
	if err := gmailSaveOAuthCredentials(path, creds); err != nil {
		return nil, err
	}
	return creds, nil
}

func gmailSaveOAuthCredentials(path string, creds *gmailOAuthCredentials) error {
	if creds == nil {
		return errors.New("credentials are required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func gmailLoadOAuthToken(path string) (*gmailOAuthToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data = trimUTF8BOM(data)
	var token gmailOAuthToken
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, err
	}
	return &token, nil
}

func gmailSaveOAuthToken(path string, token *gmailOAuthToken) error {
	if token == nil {
		return errors.New("token is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func (g *GmailCapability) tokenScopeSummary() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token == nil {
		return ""
	}
	return strings.TrimSpace(g.token.Scope)
}

func (g *GmailCapability) sendScopeAvailable() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.token == nil {
		return false
	}
	if g.creds != nil && containsString(g.creds.ScopesOrDefault(), "https://www.googleapis.com/auth/gmail.send") {
		return true
	}
	scope := strings.TrimSpace(g.token.Scope)
	if scope == "" {
		return false
	}
	for _, item := range strings.Fields(scope) {
		if strings.TrimSpace(item) == "https://www.googleapis.com/auth/gmail.send" {
			return true
		}
	}
	return false
}

func gmailNormalizeSendPermissionError(err error) error {
	if err == nil {
		return nil
	}
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(lower, "insufficient authentication scopes") ||
		strings.Contains(lower, "insufficient permission") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "forbidden") {
		return errors.New("gmail send permission is not granted for this connection; run `jot assistant auth gmail` again to reconnect with send access")
	}
	return err
}

func (g *GmailCapability) loadOrCreateCredentials() (*gmailOAuthCredentials, error) {
	g.mu.Lock()
	if g.creds != nil {
		creds := *g.creds
		g.mu.Unlock()
		return &creds, nil
	}
	g.mu.Unlock()

	creds, err := gmailLoadOAuthCredentials(g.CredPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	g.mu.Lock()
	g.creds = creds
	g.mu.Unlock()
	return creds, nil
}

func (c *gmailOAuthCredentials) ScopesOrDefault() []string {
	if c == nil || len(c.Scopes) == 0 {
		return append([]string(nil), gmailRequiredScopes...)
	}
	return append([]string(nil), c.Scopes...)
}

func (g *GmailCapability) authenticatedHTTPClient() *http.Client {
	client := &http.Client{Timeout: gmailDefaultTimeout}
	client.Transport = &gmailAuthTransport{gmail: g, base: http.DefaultTransport}
	return client
}

type gmailAuthTransport struct {
	gmail *GmailCapability
	base  http.RoundTripper
}

func (t *gmailAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	token, err := t.gmail.accessToken()
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header = clone.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	return base.RoundTrip(clone)
}

func (g *GmailCapability) accessToken() (string, error) {
	g.mu.Lock()
	token := g.token
	if token != nil && token.AccessToken != "" && time.Until(token.Expiry) > gmailTokenRefreshSlack {
		access := token.AccessToken
		g.mu.Unlock()
		return access, nil
	}
	g.mu.Unlock()

	if token == nil {
		loaded, err := gmailLoadOAuthToken(g.TokenPath)
		if err != nil {
			return "", err
		}
		g.mu.Lock()
		if g.token == nil {
			g.token = loaded
		}
		token = g.token
		g.mu.Unlock()
	}
	if token != nil && token.AccessToken != "" && time.Until(token.Expiry) > gmailTokenRefreshSlack {
		return token.AccessToken, nil
	}
	if token != nil && token.RefreshToken != "" {
		creds, err := g.loadOrCreateCredentials()
		if err != nil {
			return "", err
		}
		refreshed, err := gmailRefreshToken(creds, token.RefreshToken)
		if err != nil {
			return "", err
		}
		if refreshed.RefreshToken == "" {
			refreshed.RefreshToken = token.RefreshToken
		}
		g.mu.Lock()
		g.token = refreshed
		access := g.token.AccessToken
		g.mu.Unlock()
		_ = gmailSaveOAuthToken(g.TokenPath, refreshed)
		return access, nil
	}
	return "", errors.New("gmail is not authenticated; run `jot assistant auth gmail` first")
}

func (g *GmailCapability) requestDeviceCode(creds *gmailOAuthCredentials) (*gmailDeviceCodeResponse, error) {
	if creds == nil || strings.TrimSpace(creds.ClientID) == "" {
		return nil, errors.New("gmail client id is required; set JOT_GMAIL_CLIENT_ID or create the credentials file")
	}
	payload := url.Values{}
	payload.Set("client_id", creds.ClientID)
	payload.Set("scope", strings.Join(creds.ScopesOrDefault(), " "))

	endpoint := creds.DeviceURL
	if endpoint == "" {
		endpoint = gmailDeviceCodeURL
	}
	resp, err := http.PostForm(endpoint, payload)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var device gmailDeviceCodeResponse
	if err := gmailDecodeResponse(resp, &device); err != nil {
		return nil, err
	}
	if device.DeviceCode == "" {
		return nil, errors.New("device flow did not return a device code")
	}
	return &device, nil
}

func (g *GmailCapability) pollForToken(creds *gmailOAuthCredentials, device *gmailDeviceCodeResponse) (*gmailOAuthToken, error) {
	if creds == nil {
		return nil, errors.New("credentials are required")
	}
	pollInterval := device.Interval
	if pollInterval <= 0 {
		pollInterval = 5
	}
	deadline := time.Now().Add(time.Duration(device.ExpiresIn) * time.Second)
	endpoint := creds.TokenURL
	if endpoint == "" {
		endpoint = gmailTokenURL
	}

	for time.Now().Before(deadline) {
		form := url.Values{}
		form.Set("client_id", creds.ClientID)
		form.Set("device_code", device.DeviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")
		if strings.TrimSpace(creds.ClientSecret) != "" {
			form.Set("client_secret", creds.ClientSecret)
		}

		resp, err := http.PostForm(endpoint, form)
		if err != nil {
			return nil, err
		}

		var tokenResp gmailTokenResponse
		err = gmailDecodeResponse(resp, &tokenResp)
		if err != nil {
			resp.Body.Close()
			if tokenResp.Error == "authorization_pending" {
				time.Sleep(time.Duration(pollInterval) * time.Second)
				continue
			}
			if tokenResp.Error == "slow_down" {
				pollInterval += 5
				time.Sleep(time.Duration(pollInterval) * time.Second)
				continue
			}
			if tokenResp.Error == "access_denied" {
				return nil, errors.New("gmail authorization was denied")
			}
			if tokenResp.Error == "expired_token" {
				return nil, errors.New("gmail device code expired; run auth again")
			}
			return nil, err
		}
		resp.Body.Close()

		if tokenResp.AccessToken == "" {
			time.Sleep(time.Duration(pollInterval) * time.Second)
			continue
		}
		return &gmailOAuthToken{
			AccessToken:  tokenResp.AccessToken,
			RefreshToken: tokenResp.RefreshToken,
			TokenType:    tokenResp.TokenType,
			Scope:        tokenResp.Scope,
			Expiry:       time.Now().Add(time.Duration(max(tokenResp.ExpiresIn, 0)) * time.Second),
		}, nil
	}
	return nil, errors.New("gmail device code expired; run auth again")
}

func gmailRefreshToken(creds *gmailOAuthCredentials, refreshToken string) (*gmailOAuthToken, error) {
	form := url.Values{}
	form.Set("client_id", creds.ClientID)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	if strings.TrimSpace(creds.ClientSecret) != "" {
		form.Set("client_secret", creds.ClientSecret)
	}
	endpoint := creds.TokenURL
	if endpoint == "" {
		endpoint = gmailTokenURL
	}
	resp, err := http.PostForm(endpoint, form)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var tokenResp gmailTokenResponse
	if err := gmailDecodeResponse(resp, &tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, errors.New("refresh token response did not include an access token")
	}
	return &gmailOAuthToken{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: refreshToken,
		TokenType:    tokenResp.TokenType,
		Scope:        tokenResp.Scope,
		Expiry:       time.Now().Add(time.Duration(max(tokenResp.ExpiresIn, 0)) * time.Second),
	}, nil
}

func gmailDecodeResponse(resp *http.Response, dst any) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var apiErr gmailAPIErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil {
			if apiErr.Error.Message != "" {
				return errors.New(apiErr.Error.Message)
			}
			if apiErr.Error.Error != "" {
				return errors.New(apiErr.Error.Error)
			}
		}
		return fmt.Errorf("gmail api returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

type gmailAPIErrorResponse struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	} `json:"error"`
}

func (g *GmailCapability) profile() (*gmailProfileResponse, error) {
	g.mu.Lock()
	if g.email != "" {
		email := g.email
		g.mu.Unlock()
		return &gmailProfileResponse{EmailAddress: email}, nil
	}
	g.mu.Unlock()

	client := g.httpClient()
	if client == nil {
		return nil, errors.New("gmail is not authenticated; run `jot assistant auth gmail` first")
	}
	var profile gmailProfileResponse
	if err := g.getJSON("/gmail/v1/users/me/profile", &profile); err != nil {
		return nil, err
	}
	if profile.EmailAddress != "" {
		g.mu.Lock()
		g.email = profile.EmailAddress
		g.mu.Unlock()
	}
	return &profile, nil
}

func (g *GmailCapability) httpClient() *http.Client {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.Client != nil {
		return g.Client
	}
	if g.token == nil {
		token, err := gmailLoadOAuthToken(g.TokenPath)
		if err != nil {
			return nil
		}
		g.token = token
	}
	if g.token == nil {
		return nil
	}
	g.Client = g.authenticatedHTTPClient()
	return g.Client
}

func (g *GmailCapability) getJSON(path string, dst any) error {
	client := g.httpClient()
	if client == nil {
		return errors.New("gmail is not authenticated; run `jot assistant auth gmail` first")
	}
	req, err := http.NewRequest(http.MethodGet, g.apiURL(path), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return gmailDecodeResponse(resp, dst)
}

func (g *GmailCapability) postJSON(path string, reqBody any, dst any) error {
	client := g.httpClient()
	if client == nil {
		return errors.New("gmail is not authenticated; run `jot assistant auth gmail` first")
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, g.apiURL(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	return gmailDecodeResponse(resp, dst)
}

func (g *GmailCapability) apiURL(path string) string {
	base := strings.TrimRight(g.BaseURL, "/")
	if base == "" {
		base = gmailAPIBaseURL
	}
	return base + path
}

func (g *GmailCapability) searchMessages(query string, maxResults int) ([]NormalizedEmail, error) {
	intent := gmailBuildSearchIntent(query)
	refs, err := g.collectSearchRefs(query, intent, maxResults)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return []NormalizedEmail{}, nil
	}
	results := gmailParallelMap(refs, gmailFetchConcurrency, func(ref gmailMessageRef) (NormalizedEmail, bool) {
		msg, err := g.readMessage(ref.ID)
		if err != nil {
			return NormalizedEmail{}, false
		}
		return msg, true
	})
	sort.SliceStable(results, func(i, j int) bool {
		si := gmailMessageSemanticScore(results[i], intent)
		sj := gmailMessageSemanticScore(results[j], intent)
		if si != sj {
			return si > sj
		}
		if results[i].Date.Equal(results[j].Date) {
			return results[i].ID > results[j].ID
		}
		if results[i].Date.IsZero() {
			return false
		}
		if results[j].Date.IsZero() {
			return true
		}
		return results[i].Date.After(results[j].Date)
	})
	return results, nil
}

func (g *GmailCapability) collectSearchRefs(query string, intent gmailSearchIntent, maxResults int) ([]gmailMessageRef, error) {
	var merged []gmailMessageRef
	seen := make(map[string]struct{})
	queries := []string{strings.TrimSpace(query)}
	if g.ProgressFn != nil && intent.ExactFact {
		queries = gmailSearchQueryVariants(query, intent)
	}
	for idx, q := range queries {
		if strings.TrimSpace(q) == "" {
			continue
		}
		refs, err := g.fetchSearchRefs(q, maxResults)
		if err != nil {
			if idx == 0 && len(merged) == 0 {
				return nil, err
			}
			continue
		}
		for _, ref := range refs {
			if ref.ID == "" {
				continue
			}
			if _, ok := seen[ref.ID]; ok {
				continue
			}
			seen[ref.ID] = struct{}{}
			merged = append(merged, ref)
			if maxResults > 0 && len(merged) >= maxResults {
				return merged[:maxResults], nil
			}
		}
		if len(merged) > 0 && !intent.ExactFact {
			break
		}
	}
	return merged, nil
}

func (g *GmailCapability) fetchSearchRefs(query string, maxResults int) ([]gmailMessageRef, error) {
	var resp gmailListMessagesResponse
	escaped := url.QueryEscape(query)
	path := fmt.Sprintf("/gmail/v1/users/me/messages?q=%s&maxResults=%d", escaped, maxResults)
	if err := g.getJSON(path, &resp); err != nil {
		return nil, err
	}
	if len(resp.Messages) == 0 {
		return []gmailMessageRef{}, nil
	}
	if maxResults > 0 && len(resp.Messages) > maxResults {
		resp.Messages = resp.Messages[:maxResults]
	}
	return resp.Messages, nil
}

type gmailSearchIntent struct {
	Raw               string
	Terms             []string
	ExpandedTerms     []string
	PreferAttachments bool
	PreferImages      bool
	ExactFact         bool
}

func gmailBuildSearchIntent(query string) gmailSearchIntent {
	raw := strings.ToLower(strings.TrimSpace(query))
	intent := gmailSearchIntent{Raw: raw}
	if raw == "" {
		return intent
	}
	intent.Terms = gmailSearchTokens(raw)
	intent.ExpandedTerms = append([]string(nil), intent.Terms...)
	for _, term := range intent.Terms {
		switch term {
		case "passport":
			intent.ExactFact = true
			intent.PreferAttachments = true
			intent.PreferImages = true
			intent.ExpandedTerms = append(intent.ExpandedTerms,
				"passport number",
				"passport no",
				"passport application",
				"passport confirmation",
				"travel document",
				"identity document",
			)
		case "brp", "biometric", "residence", "permit":
			intent.ExactFact = true
			intent.PreferAttachments = true
			intent.PreferImages = true
			intent.ExpandedTerms = append(intent.ExpandedTerms,
				"biometric residence permit",
				"residence permit",
				"identity document",
				"travel document",
			)
		case "visa":
			intent.ExactFact = true
			intent.PreferAttachments = true
			intent.PreferImages = true
			intent.ExpandedTerms = append(intent.ExpandedTerms,
				"visa application",
				"travel document",
				"immigration",
				"entry clearance",
			)
		case "id", "identity":
			intent.ExactFact = true
			intent.PreferAttachments = true
			intent.PreferImages = true
			intent.ExpandedTerms = append(intent.ExpandedTerms,
				"identity document",
				"passport",
				"government id",
				"id card",
			)
		case "document", "documents", "attachment", "attachments", "scan", "photo", "image":
			intent.PreferAttachments = true
			if term == "image" || term == "photo" || term == "scan" {
				intent.PreferImages = true
			}
		case "reference", "number", "number?", "confirmation":
			intent.ExactFact = true
		}
	}
	if containsAnyTerm(intent.Terms, "passport", "brp", "visa", "permit", "identity", "id") {
		intent.PreferAttachments = true
		intent.PreferImages = true
		intent.ExactFact = true
	}
	intent.ExpandedTerms = uniqueTrimmedStrings(intent.ExpandedTerms)
	return intent
}

func gmailSearchTokens(input string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(input)), func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == ':' || r == '(' || r == ')' || r == '[' || r == ']' || r == '{' || r == '}' || r == '"' || r == '\''
	})
	tokens := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if gmailIsQueryBoundaryWord(part) {
			continue
		}
		tokens = append(tokens, part)
	}
	return uniqueTrimmedStrings(tokens)
}

func gmailMessageSemanticScore(msg NormalizedEmail, intent gmailSearchIntent) int {
	score := 0
	haystackParts := []string{
		strings.ToLower(strings.TrimSpace(msg.Subject)),
		strings.ToLower(strings.TrimSpace(msg.From)),
		strings.ToLower(strings.TrimSpace(msg.Snippet)),
		strings.ToLower(strings.TrimSpace(msg.BodyText)),
		strings.ToLower(strings.TrimSpace(msg.BodyHTML)),
	}
	for _, link := range msg.Links {
		haystackParts = append(haystackParts, strings.ToLower(strings.TrimSpace(link.Label)), strings.ToLower(strings.TrimSpace(link.Context)), strings.ToLower(strings.TrimSpace(link.URL)))
	}
	for _, att := range msg.Attachments {
		haystackParts = append(haystackParts,
			strings.ToLower(strings.TrimSpace(att.Filename)),
			strings.ToLower(strings.TrimSpace(att.MimeType)),
		)
	}
	haystack := strings.Join(haystackParts, " ")
	for _, term := range intent.ExpandedTerms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(haystack, term) {
			score += 6
		}
	}
	for _, term := range intent.Terms {
		term = strings.ToLower(strings.TrimSpace(term))
		if term == "" {
			continue
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(msg.Subject)), term) {
			score += 7
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(msg.Snippet)), term) {
			score += 4
		}
		if strings.Contains(strings.ToLower(strings.TrimSpace(msg.BodyText)), term) {
			score += 4
		}
	}
	if intent.PreferAttachments && len(msg.Attachments) > 0 {
		score += 3
	}
	for _, att := range msg.Attachments {
		score += gmailAttachmentMetaSemanticScore(att)
	}
	if intent.PreferImages && gmailMessageHasImageAttachment(msg) {
		score += 4
	}
	if intent.ExactFact && gmailMessageHasIdentityAttachment(msg) {
		score += 5
	}
	score += gmailMessageNoisePenalty(msg)
	if !msg.Date.IsZero() {
		score += 1
	}
	return score
}

func gmailMessageHasImageAttachment(msg NormalizedEmail) bool {
	for _, att := range msg.Attachments {
		if gmailAttachmentLooksLikeImage(att) {
			return true
		}
	}
	return false
}

func gmailMessageHasIdentityAttachment(msg NormalizedEmail) bool {
	for _, att := range msg.Attachments {
		if gmailAttachmentLooksLikeIdentityDoc(att) {
			return true
		}
	}
	return false
}

func gmailMessageNoisePenalty(msg NormalizedEmail) int {
	text := strings.ToLower(strings.Join([]string{msg.Subject, msg.Snippet, msg.BodyText}, " "))
	penalty := 0
	for _, token := range []string{
		"newsletter",
		"digest",
		"morning brew",
		"quora digest",
		"promo",
		"promotion",
		"sale",
		"unsubscribe",
		"marketing",
		"social",
		"notification",
	} {
		if strings.Contains(text, token) {
			penalty += 4
		}
	}
	return penalty
}

func gmailAttachmentMetaSemanticScore(meta AttachmentMeta) int {
	score := 0
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	mime := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if gmailAttachmentLooksLikeIdentityDoc(meta) {
		score += 8
	}
	if gmailAttachmentLooksLikeImage(meta) {
		score += 3
	}
	if gmailAttachmentLooksLikeDocument(meta) {
		score += 4
	}
	if gmailAttachmentLooksLikeNoise(meta) {
		score -= 6
	}
	if strings.Contains(name, "passport") || strings.Contains(name, "visa") || strings.Contains(name, "brp") || strings.Contains(name, "permit") || strings.Contains(name, "id") {
		score += 8
	}
	if strings.Contains(mime, "pdf") || strings.Contains(mime, "document") {
		score += 2
	}
	return score
}

func gmailAttachmentSourcePriority(source gmailAttachmentSource) int {
	score := gmailMessageSemanticScore(source.Email, gmailBuildSearchIntent(source.Email.Subject+" "+source.Email.Snippet))
	if len(source.Attachments) > 0 {
		score += 3
	}
	for _, att := range source.Attachments {
		score += gmailAttachmentMetaSemanticScore(att)
	}
	return score
}

func gmailAttachmentSelectionPriority(selection gmailAttachmentSelection) int {
	score := gmailMessageSemanticScore(selectionToEmailStub(selection), gmailBuildSearchIntent(selection.Subject+" "+selection.From+" "+selection.Attachment.Filename))
	score += gmailAttachmentMetaSemanticScore(selection.Attachment)
	return score
}

func selectionToEmailStub(selection gmailAttachmentSelection) NormalizedEmail {
	return NormalizedEmail{
		Subject:     selection.Subject,
		From:        selection.From,
		Snippet:     selection.Subject,
		BodyText:    selection.Subject,
		Attachments: []AttachmentMeta{selection.Attachment},
	}
}

func gmailAttachmentLooksLikeDocument(meta AttachmentMeta) bool {
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	mime := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if strings.Contains(mime, "pdf") || strings.Contains(mime, "word") || strings.Contains(mime, "document") || strings.Contains(mime, "sheet") || strings.Contains(mime, "excel") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf", ".doc", ".docx", ".rtf", ".txt", ".csv", ".xls", ".xlsx", ".odt":
		return true
	default:
		return false
	}
}

func gmailAttachmentLooksLikeImage(meta AttachmentMeta) bool {
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	mime := strings.ToLower(strings.TrimSpace(meta.MimeType))
	if strings.HasPrefix(mime, "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tif", ".tiff", ".webp":
		return true
	default:
		return false
	}
}

func gmailAttachmentLooksLikeIdentityDoc(meta AttachmentMeta) bool {
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	for _, token := range []string{"passport", "visa", "brp", "biometric", "permit", "identity", "id", "travel document", "document"} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return gmailAttachmentLooksLikeImage(meta) || gmailAttachmentLooksLikeDocument(meta)
}

func gmailAttachmentLooksLikeNoise(meta AttachmentMeta) bool {
	name := strings.ToLower(strings.TrimSpace(meta.Filename))
	for _, token := range []string{
		"logo",
		"header",
		"footer",
		"icon",
		"image001",
		"image002",
		"webwb",
		"newsletter",
		"digest",
		"unsubscribe",
		"statement_",
		"statement ",
		"statement",
		"invoice",
		"booking confirmation",
		"reservation",
		"hotel reservation",
		"invite.ics",
	} {
		if strings.Contains(name, token) {
			return true
		}
	}
	return false
}

func containsAnyTerm(tokens []string, candidates ...string) bool {
	for _, token := range tokens {
		for _, candidate := range candidates {
			if strings.EqualFold(strings.TrimSpace(token), strings.TrimSpace(candidate)) {
				return true
			}
		}
	}
	return false
}

func (g *GmailCapability) readRawMessage(id string) (*gmailMessage, error) {
	var msg gmailMessage
	if err := g.getJSON("/gmail/v1/users/me/messages/"+url.PathEscape(id)+"?format=full", &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

func (g *GmailCapability) readMessage(id string) (NormalizedEmail, error) {
	msg, err := g.readRawMessage(id)
	if err != nil {
		return NormalizedEmail{}, err
	}
	return gmailNormalizeMessage(*msg), nil
}

func (g *GmailCapability) readThread(id string) (gmailThreadResult, error) {
	var raw struct {
		ID       string         `json:"id"`
		Messages []gmailMessage `json:"messages"`
	}
	if err := g.getJSON("/gmail/v1/users/me/threads/"+url.PathEscape(id)+"?format=full", &raw); err != nil {
		return gmailThreadResult{}, err
	}
	result := gmailThreadResult{ThreadID: raw.ID, Messages: make([]NormalizedEmail, 0, len(raw.Messages))}
	if result.ThreadID == "" {
		result.ThreadID = id
	}
	result.MessageCount = len(raw.Messages)
	participants := make(map[string]struct{})
	subject := ""
	var earliest, latest time.Time
	for _, msg := range raw.Messages {
		email := gmailNormalizeMessage(msg)
		if subject == "" && strings.TrimSpace(email.Subject) != "" {
			subject = strings.TrimSpace(email.Subject)
		}
		if !email.Date.IsZero() {
			if earliest.IsZero() || email.Date.Before(earliest) {
				earliest = email.Date
			}
			if latest.IsZero() || email.Date.After(latest) {
				latest = email.Date
			}
		}
		if email.From != "" {
			participants[email.From] = struct{}{}
		}
		for _, addr := range email.To {
			if strings.TrimSpace(addr) != "" {
				participants[strings.TrimSpace(addr)] = struct{}{}
			}
		}
		result.Messages = append(result.Messages, email)
	}
	sort.SliceStable(result.Messages, func(i, j int) bool {
		if result.Messages[i].Date.Equal(result.Messages[j].Date) {
			return result.Messages[i].ID > result.Messages[j].ID
		}
		if result.Messages[i].Date.IsZero() {
			return false
		}
		if result.Messages[j].Date.IsZero() {
			return true
		}
		return result.Messages[i].Date.After(result.Messages[j].Date)
	})
	if subject == "" && len(result.Messages) > 0 {
		subject = strings.TrimSpace(result.Messages[0].Subject)
	}
	result.Subject = subject
	result.EarliestDate = earliest
	result.LatestDate = latest
	result.AttachmentCount = gmailCountThreadAttachments(result.Messages)
	result.Participants = make([]string, 0, len(participants))
	for participant := range participants {
		result.Participants = append(result.Participants, participant)
	}
	sort.Strings(result.Participants)
	return result, nil
}

func (g *GmailCapability) downloadAttachment(messageID, attachmentID string) (AttachmentMeta, []byte, error) {
	msg, err := g.readRawMessage(messageID)
	if err != nil {
		return AttachmentMeta{}, nil, err
	}
	meta, ok := gmailFindAttachment(*msg, attachmentID)
	if !ok {
		return AttachmentMeta{}, nil, fmt.Errorf("attachment %q not found on message %q", attachmentID, messageID)
	}
	data, err := g.downloadAttachmentData(messageID, attachmentID)
	if err != nil {
		return AttachmentMeta{}, nil, err
	}
	return meta, data, nil
}

func (g *GmailCapability) downloadAttachmentData(messageID, attachmentID string) ([]byte, error) {
	var att gmailAttachmentResponse
	path := fmt.Sprintf("/gmail/v1/users/me/messages/%s/attachments/%s", url.PathEscape(messageID), url.PathEscape(attachmentID))
	if err := g.getJSON(path, &att); err != nil {
		return nil, err
	}
	data, err := gmailDecodeAttachmentData(att.Data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (g *GmailCapability) createDraft(raw string) (gmailDraftResponse, error) {
	var draft gmailDraftResponse
	req := gmailDraftRequest{Message: gmailRawMessage{Raw: raw}}
	if err := g.postJSON("/gmail/v1/users/me/drafts", req, &draft); err != nil {
		return gmailDraftResponse{}, err
	}
	return draft, nil
}

func (g *GmailCapability) sendRawMessage(raw string) (gmailSendResponse, error) {
	var out gmailSendResponse
	if err := g.postJSON("/gmail/v1/users/me/messages/send", gmailDraftRequest{Message: gmailRawMessage{Raw: raw}}, &out); err != nil {
		return gmailSendResponse{}, err
	}
	return out, nil
}

func gmailNormalizeMessage(msg gmailMessage) NormalizedEmail {
	headers := msg.Payload.Headers
	subject := gmailHeaderValue(headers, "Subject")
	from := gmailAddressHeader(headers, "From")
	to := gmailAddressListHeader(headers, "To")
	bodyText := gmailMessageBodyText(msg.Payload)
	bodyHTML := gmailMessageBodyHTML(msg.Payload)
	if bodyText == "" {
		bodyText = strings.TrimSpace(msg.Snippet)
	}
	links := gmailExtractLinks(bodyHTML, bodyText)
	attachments := gmailCollectAttachments(msg.Payload, msg.ID)
	labelIDs := append([]string(nil), msg.LabelIDs...)
	sort.Strings(labelIDs)

	return NormalizedEmail{
		ID:          msg.ID,
		ThreadID:    msg.ThreadID,
		Subject:     subject,
		From:        from,
		To:          to,
		Date:        gmailParseMessageDate(msg),
		BodyText:    bodyText,
		BodyHTML:    bodyHTML,
		Snippet:     strings.TrimSpace(msg.Snippet),
		Labels:      labelIDs,
		Links:       links,
		Attachments: attachments,
		Unread:      containsString(msg.LabelIDs, "UNREAD"),
	}
}

func gmailParseMessageDate(msg gmailMessage) time.Time {
	if msg.InternalDate != "" {
		if ms, err := strconv.ParseInt(msg.InternalDate, 10, 64); err == nil && ms > 0 {
			return time.Unix(0, ms*int64(time.Millisecond))
		}
	}
	value := gmailHeaderValue(msg.Payload.Headers, "Date")
	if value != "" {
		if parsed, err := mail.ParseDate(value); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func gmailParallelMap[T any, R any](items []T, concurrency int, fn func(T) (R, bool)) []R {
	if len(items) == 0 {
		return nil
	}
	if concurrency <= 1 || len(items) == 1 {
		out := make([]R, 0, len(items))
		for _, item := range items {
			value, ok := fn(item)
			if ok {
				out = append(out, value)
			}
		}
		return out
	}
	if concurrency > len(items) {
		concurrency = len(items)
	}

	type result struct {
		index int
		value R
		ok    bool
	}

	sem := make(chan struct{}, concurrency)
	results := make(chan result, len(items))
	var wg sync.WaitGroup
	for index, item := range items {
		wg.Add(1)
		go func(idx int, current T) {
			defer wg.Done()
			sem <- struct{}{}
			value, ok := fn(current)
			<-sem
			results <- result{index: idx, value: value, ok: ok}
		}(index, item)
	}
	wg.Wait()
	close(results)

	ordered := make([]result, len(items))
	for item := range results {
		ordered[item.index] = item
	}
	out := make([]R, 0, len(items))
	for _, item := range ordered {
		if item.ok {
			out = append(out, item.value)
		}
	}
	return out
}

func gmailHeaderValue(headers []gmailHeader, name string) string {
	for _, header := range headers {
		if strings.EqualFold(header.Name, name) {
			return strings.TrimSpace(header.Value)
		}
	}
	return ""
}

func gmailAddressHeader(headers []gmailHeader, name string) string {
	value := gmailHeaderValue(headers, name)
	if value == "" {
		return ""
	}
	addrs, err := mail.ParseAddressList(value)
	if err != nil || len(addrs) == 0 {
		return value
	}
	return addrs[0].String()
}

func gmailAddressListHeader(headers []gmailHeader, name string) []string {
	value := gmailHeaderValue(headers, name)
	if value == "" {
		return nil
	}
	addrs, err := mail.ParseAddressList(value)
	if err != nil || len(addrs) == 0 {
		return []string{value}
	}
	parts := make([]string, 0, len(addrs))
	for _, addr := range addrs {
		parts = append(parts, addr.String())
	}
	return parts
}

func gmailMessageBodyText(part gmailMessagePart) string {
	if part.MimeType == "text/plain" {
		if text := gmailDecodePartText(part.Body.Data); text != "" {
			return strings.TrimSpace(text)
		}
	}
	if part.MimeType == "text/html" {
		if text := gmailDecodePartText(part.Body.Data); text != "" {
			return strings.TrimSpace(gmailStripHTML(text))
		}
	}
	if len(part.Parts) > 0 {
		plain := make([]string, 0, len(part.Parts))
		htmlParts := make([]string, 0, len(part.Parts))
		for _, child := range part.Parts {
			text := gmailMessageBodyText(child)
			switch {
			case child.MimeType == "text/plain" && text != "":
				plain = append(plain, text)
			case child.MimeType == "text/html" && text != "":
				htmlParts = append(htmlParts, text)
			case text != "":
				plain = append(plain, text)
			}
		}
		if len(plain) > 0 {
			return strings.TrimSpace(strings.Join(plain, "\n\n"))
		}
		if len(htmlParts) > 0 {
			return strings.TrimSpace(strings.Join(htmlParts, "\n\n"))
		}
	}
	if text := gmailDecodePartText(part.Body.Data); text != "" {
		if strings.Contains(strings.ToLower(part.MimeType), "html") {
			return strings.TrimSpace(gmailStripHTML(text))
		}
		return strings.TrimSpace(text)
	}
	return ""
}

func gmailMessageBodyHTML(part gmailMessagePart) string {
	if part.MimeType == "text/html" {
		if text := gmailDecodePartText(part.Body.Data); text != "" {
			return strings.TrimSpace(text)
		}
	}
	if len(part.Parts) > 0 {
		for _, child := range part.Parts {
			if text := gmailMessageBodyHTML(child); text != "" {
				return text
			}
		}
	}
	if text := gmailDecodePartText(part.Body.Data); text != "" && strings.Contains(strings.ToLower(part.MimeType), "html") {
		return strings.TrimSpace(text)
	}
	return ""
}

func gmailExtractLinks(bodyHTML, bodyText string) []EmailLink {
	var links []EmailLink
	seen := map[string]struct{}{}
	appendLink := func(rawURL, label string) {
		rawURL = strings.TrimSpace(strings.Trim(rawURL, `"'`))
		if rawURL == "" {
			return
		}
		if _, ok := seen[strings.ToLower(rawURL)]; ok {
			return
		}
		seen[strings.ToLower(rawURL)] = struct{}{}
		context := surroundingTextWindow(bodyText, label, 180)
		if context == "" {
			context = surroundingTextWindow(bodyText, rawURL, 180)
		}
		links = append(links, EmailLink{
			URL:     rawURL,
			Label:   strings.TrimSpace(gmailStripHTML(label)),
			Context: context,
		})
	}
	if strings.TrimSpace(bodyHTML) != "" {
		for _, match := range gmailExtractHTMLAnchors(bodyHTML) {
			appendLink(match.URL, match.Label)
		}
	}
	for _, rawURL := range extractPlainURLs(bodyText) {
		appendLink(rawURL, rawURL)
	}
	return links
}

func gmailCollectAttachments(part gmailMessagePart, messageID string) []AttachmentMeta {
	var out []AttachmentMeta
	if meta, ok := gmailAttachmentMetaFromPart(part, messageID); ok {
		out = append(out, meta)
	}
	for _, child := range part.Parts {
		out = append(out, gmailCollectAttachments(child, messageID)...)
	}
	return out
}

func gmailAttachmentMetaFromPart(part gmailMessagePart, messageID string) (AttachmentMeta, bool) {
	attachmentID := strings.TrimSpace(part.Body.AttachmentID)
	filename := strings.TrimSpace(part.Filename)
	if attachmentID == "" || filename == "" {
		return AttachmentMeta{}, false
	}
	return AttachmentMeta{
		Filename:     filename,
		MimeType:     strings.TrimSpace(part.MimeType),
		SizeBytes:    part.Body.Size,
		AttachmentID: attachmentID,
		MessageID:    messageID,
	}, true
}

func gmailFindAttachment(msg gmailMessage, attachmentID string) (AttachmentMeta, bool) {
	var walk func(gmailMessagePart) (AttachmentMeta, bool)
	walk = func(part gmailMessagePart) (AttachmentMeta, bool) {
		if meta, ok := gmailAttachmentMetaFromPart(part, msg.ID); ok && meta.AttachmentID == attachmentID {
			return meta, true
		}
		for _, child := range part.Parts {
			if meta, ok := walk(child); ok {
				return meta, true
			}
		}
		return AttachmentMeta{}, false
	}
	return walk(msg.Payload)
}

func gmailDecodeAttachmentData(data string) ([]byte, error) {
	if strings.TrimSpace(data) == "" {
		return nil, nil
	}
	if decoded, err := base64.RawURLEncoding.DecodeString(data); err == nil {
		return decoded, nil
	}
	if decoded, err := base64.URLEncoding.DecodeString(data); err == nil {
		return decoded, nil
	}
	return nil, errors.New("could not decode attachment payload")
}

func gmailDecodePartText(data string) string {
	data = strings.TrimSpace(data)
	if data == "" {
		return ""
	}
	decoded, err := gmailDecodeAttachmentData(data)
	if err != nil {
		return data
	}
	return string(decoded)
}

func gmailStripHTML(input string) string {
	if input == "" {
		return ""
	}
	s := gmailHTMLToText(input)
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = gmailNormalizeHTMLTextSpacing(s)
	return strings.TrimSpace(s)
}

type gmailHTMLAnchor struct {
	URL   string
	Label string
}

func gmailExtractHTMLAnchors(bodyHTML string) []gmailHTMLAnchor {
	var out []gmailHTMLAnchor
	lower := strings.ToLower(bodyHTML)
	offset := 0
	for {
		start := strings.Index(lower[offset:], "<a")
		if start < 0 {
			break
		}
		start += offset
		tagEnd := strings.Index(lower[start:], ">")
		if tagEnd < 0 {
			break
		}
		tagEnd += start
		tag := bodyHTML[start : tagEnd+1]
		href := gmailHTMLAttributeValue(tag, "href")
		if href == "" {
			offset = tagEnd + 1
			continue
		}
		closeIdx := strings.Index(lower[tagEnd+1:], "</a>")
		label := href
		if closeIdx >= 0 {
			closeIdx += tagEnd + 1
			label = bodyHTML[tagEnd+1 : closeIdx]
			offset = closeIdx + len("</a>")
		} else {
			offset = tagEnd + 1
		}
		out = append(out, gmailHTMLAnchor{
			URL:   strings.TrimSpace(href),
			Label: strings.TrimSpace(label),
		})
	}
	return out
}

func gmailHTMLAttributeValue(tag, attr string) string {
	lower := strings.ToLower(tag)
	needle := strings.ToLower(strings.TrimSpace(attr))
	if needle == "" {
		return ""
	}
	for i := 0; i < len(lower); i++ {
		idx := strings.Index(lower[i:], needle)
		if idx < 0 {
			return ""
		}
		idx += i
		if idx > 0 && isHTMLAttrNameChar(rune(lower[idx-1])) {
			i = idx + len(needle)
			continue
		}
		endName := idx + len(needle)
		if endName < len(lower) && isHTMLAttrNameChar(rune(lower[endName])) {
			i = endName
			continue
		}
		j := endName
		for j < len(tag) && unicode.IsSpace(rune(tag[j])) {
			j++
		}
		if j >= len(tag) || tag[j] != '=' {
			i = endName
			continue
		}
		j++
		for j < len(tag) && unicode.IsSpace(rune(tag[j])) {
			j++
		}
		if j >= len(tag) {
			return ""
		}
		quote := tag[j]
		if quote == '"' || quote == '\'' {
			j++
			start := j
			for j < len(tag) && tag[j] != quote {
				j++
			}
			return tag[start:j]
		}
		start := j
		for j < len(tag) && !unicode.IsSpace(rune(tag[j])) && tag[j] != '>' {
			j++
		}
		return tag[start:j]
	}
	return ""
}

func isHTMLAttrNameChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == ':'
}

func gmailHTMLToText(input string) string {
	var out strings.Builder
	lower := strings.ToLower(input)
	for i := 0; i < len(input); {
		if input[i] != '<' {
			out.WriteByte(input[i])
			i++
			continue
		}
		tagEnd := strings.IndexByte(input[i:], '>')
		if tagEnd < 0 {
			break
		}
		tagEnd += i
		tagContent := strings.TrimSpace(input[i+1 : tagEnd])
		tagLower := strings.ToLower(tagContent)
		if strings.HasPrefix(tagLower, "!--") {
			endComment := strings.Index(lower[tagEnd+1:], "-->")
			if endComment < 0 {
				break
			}
			i = tagEnd + 1 + endComment + 3
			continue
		}
		tagName, closing := gmailHTMLTagName(tagLower)
		switch tagName {
		case "script", "style":
			closeTag := "</" + tagName + ">"
			closeIdx := strings.Index(lower[tagEnd+1:], closeTag)
			if closeIdx < 0 {
				i = tagEnd + 1
			} else {
				i = tagEnd + 1 + closeIdx + len(closeTag)
			}
			continue
		case "br":
			out.WriteByte('\n')
		case "p", "div", "tr":
			if closing {
				out.WriteByte('\n')
			}
		case "li":
			if closing {
				out.WriteByte('\n')
			} else {
				out.WriteString(" - ")
			}
		case "td", "th":
			if closing {
				out.WriteByte('\t')
			}
		}
		i = tagEnd + 1
	}
	return out.String()
}

func gmailHTMLTagName(tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", false
	}
	closing := false
	if strings.HasPrefix(tag, "/") {
		closing = true
		tag = strings.TrimSpace(tag[1:])
	}
	end := 0
	for end < len(tag) && isHTMLAttrNameChar(rune(tag[end])) {
		end++
	}
	return tag[:end], closing
}

func gmailNormalizeHTMLTextSpacing(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	var out []string
	blank := false
	for _, line := range lines {
		if line == "" {
			if !blank {
				out = append(out, "")
				blank = true
			}
			continue
		}
		out = append(out, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

func gmailResolveAttachmentSavePath(saveDir, filename, attachmentID string) (string, error) {
	saveDir = strings.TrimSpace(saveDir)
	if saveDir == "" {
		saveDir = "."
	}
	filename = gmailSafeAttachmentFilename(filename, attachmentID)
	info, err := os.Stat(saveDir)
	if err == nil && info.IsDir() {
		return gmailUniquePath(filepath.Join(saveDir, filename), attachmentID), nil
	}
	if err == nil && !info.IsDir() {
		return saveDir, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		if filepath.Ext(saveDir) != "" {
			return saveDir, nil
		}
		if err := os.MkdirAll(saveDir, 0o755); err != nil {
			return "", err
		}
		return gmailUniquePath(filepath.Join(saveDir, filename), attachmentID), nil
	}
	return "", err
}

func gmailSafeAttachmentFilename(filename, attachmentID string) string {
	name := strings.TrimSpace(filepath.Base(filename))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r == '/' || r == '\\':
			b.WriteByte('_')
		case unicode.IsControl(r):
			continue
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimSpace(b.String())
	if name == "" {
		name = "attachment"
	}
	if filepath.Ext(name) == "" && strings.TrimSpace(attachmentID) != "" {
		name += "-" + attachmentID
	}
	return name
}

func gmailUniquePath(path string, seed string) string {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)
	dir := filepath.Dir(path)
	for i := 2; i < 1000; i++ {
		next := filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(next); errors.Is(err, os.ErrNotExist) {
			return next
		}
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s%s", base, seed, ext))
}

func gmailOneLineSummary(msg NormalizedEmail) string {
	when := ""
	if !msg.Date.IsZero() {
		when = msg.Date.Format("3:04pm")
	}
	if when != "" {
		return fmt.Sprintf("%s  %s — %s", msg.From, msg.Subject, when)
	}
	return fmt.Sprintf("%s  %s", msg.From, msg.Subject)
}

func (g *GmailCapability) inferActions(text string, now time.Time) ExtractedActions {
	if summary, err := g.semanticExtractActions(text, now); err == nil {
		return summary
	}
	// Keep the local extractor as a fallback only, so normal assistant behavior
	// stays model-driven even when Gmail tools expose action extraction directly.
	return ExtractActionsAt(text, now)
}

func (g *GmailCapability) semanticExtractActions(text string, now time.Time) (ExtractedActions, error) {
	if strings.TrimSpace(text) == "" {
		return ExtractedActions{}, errors.New("text is required")
	}
	provider, err := NewModelProvider(g.Config)
	if err != nil {
		return ExtractedActions{}, err
	}
	payload, err := json.MarshalIndent(map[string]any{
		"currentTime": now.Format(time.RFC3339),
		"text":        truncateForPrompt(text, 12000),
	}, "", "  ")
	if err != nil {
		return ExtractedActions{}, err
	}
	messages := []Message{
		{
			Role: "system",
			Content: strings.TrimSpace(`Extract actionable information from an email or attachment semantically.
Return exactly one JSON object and nothing else.
Schema:
{
  "summary": "short paragraph",
  "actionItems": ["..."],
  "deadlines": [{"task":"...", "raw":"..."}],
  "meetingReqs": [{"subject":"...", "proposedTimes":["..."], "participants":["..."], "location":"..."}],
  "entities": [{"type":"person|company|amount|date", "value":"..."}]
}
Rules:
- Use meaning, not keyword matching.
- Ignore disclaimers, signatures, legal boilerplate, and generic newsletter text.
- Only include actions a human should realistically do.
- Keep output concise and high signal.
- If there are no deadlines or meetings, return empty arrays.`),
		},
		{Role: "user", Content: string(payload)},
	}
	response, err := provider.Chat(messages, nil)
	if err != nil {
		return ExtractedActions{}, err
	}
	return parseSemanticExtractedActions(response)
}

func parseSemanticExtractedActions(raw string) (ExtractedActions, error) {
	type semanticDeadline struct {
		Task string `json:"task"`
		Raw  string `json:"raw"`
	}
	type semanticMeeting struct {
		Subject       string   `json:"subject"`
		ProposedTimes []string `json:"proposedTimes"`
		Participants  []string `json:"participants"`
		Location      string   `json:"location"`
	}
	type semanticEntity struct {
		Type  string `json:"type"`
		Value string `json:"value"`
	}
	type semanticActions struct {
		Summary     string             `json:"summary"`
		ActionItems []string           `json:"actionItems"`
		Deadlines   []semanticDeadline `json:"deadlines"`
		MeetingReqs []semanticMeeting  `json:"meetingReqs"`
		Entities    []semanticEntity   `json:"entities"`
	}

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ExtractedActions{}, errors.New("empty semantic extraction response")
	}
	var decoded semanticActions
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start < 0 || end <= start {
			return ExtractedActions{}, err
		}
		obj, parseErr := parseJSONObject(raw[start : end+1])
		if parseErr != nil {
			return ExtractedActions{}, err
		}
		data, marshalErr := json.Marshal(obj)
		if marshalErr != nil {
			return ExtractedActions{}, marshalErr
		}
		if err := json.Unmarshal(data, &decoded); err != nil {
			return ExtractedActions{}, err
		}
	}

	out := ExtractedActions{
		Summary:     strings.TrimSpace(decoded.Summary),
		ActionItems: compactStrings(decoded.ActionItems),
	}
	for _, item := range decoded.Deadlines {
		task := strings.TrimSpace(item.Task)
		raw := strings.TrimSpace(item.Raw)
		if task == "" && raw == "" {
			continue
		}
		out.Deadlines = append(out.Deadlines, Deadline{Task: task, Raw: raw})
	}
	for _, item := range decoded.MeetingReqs {
		req := MeetingRequest{
			Subject:      strings.TrimSpace(item.Subject),
			Participants: compactStrings(item.Participants),
			Location:     strings.TrimSpace(item.Location),
		}
		for _, slot := range compactStrings(item.ProposedTimes) {
			req.ProposedTimes = append(req.ProposedTimes, TimeSlot{Raw: slot})
		}
		if req.Subject == "" && len(req.ProposedTimes) == 0 && len(req.Participants) == 0 && req.Location == "" {
			continue
		}
		out.MeetingReqs = append(out.MeetingReqs, req)
	}
	for _, item := range decoded.Entities {
		entity := Entity{Type: strings.TrimSpace(item.Type), Value: strings.TrimSpace(item.Value)}
		if entity.Type == "" || entity.Value == "" {
			continue
		}
		out.Entities = append(out.Entities, entity)
	}
	if strings.TrimSpace(out.Summary) == "" {
		out.Summary = buildActionSummary(out.ActionItems, out.Deadlines, out.MeetingReqs, splitLines(strings.Join(out.ActionItems, "\n")))
	}
	return out, nil
}

func gmailActionSummary(actions ExtractedActions) string {
	if strings.TrimSpace(actions.Summary) != "" {
		return actions.Summary
	}
	return fmt.Sprintf("%d action items, %d deadlines, %d meeting requests", len(actions.ActionItems), len(actions.Deadlines), len(actions.MeetingReqs))
}

func gmailComposeReplyRaw(msg *gmailMessage, body string, subject string) (string, error) {
	if msg == nil {
		return "", errors.New("message is required")
	}
	replyTo := gmailHeaderValue(msg.Payload.Headers, "Reply-To")
	if strings.TrimSpace(replyTo) == "" {
		replyTo = gmailHeaderValue(msg.Payload.Headers, "From")
	}
	if strings.TrimSpace(replyTo) == "" {
		return "", errors.New("original message does not include a reply address")
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "Re:"
	}
	messageID := strings.TrimSpace(gmailHeaderValue(msg.Payload.Headers, "Message-ID"))
	references := strings.TrimSpace(gmailHeaderValue(msg.Payload.Headers, "References"))
	if messageID != "" {
		if references != "" {
			references += " "
		}
		references += messageID
	}

	var b strings.Builder
	fmt.Fprintf(&b, "To: %s\r\n", replyTo)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	if messageID != "" {
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", messageID)
	}
	if references != "" {
		fmt.Fprintf(&b, "References: %s\r\n", references)
	}
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\r\n")

	return base64.RawURLEncoding.EncodeToString([]byte(b.String())), nil
}

func gmailComposeRaw(to, subject, body string, attachmentPaths []string) (string, error) {
	to = strings.TrimSpace(to)
	subject = strings.TrimSpace(subject)
	body = strings.TrimSpace(body)
	if to == "" {
		return "", errors.New("to is required")
	}
	if subject == "" {
		return "", errors.New("subject is required")
	}
	if body == "" {
		return "", errors.New("body is required")
	}
	if len(attachmentPaths) == 0 {
		var b strings.Builder
		fmt.Fprintf(&b, "To: %s\r\n", to)
		fmt.Fprintf(&b, "Subject: %s\r\n", subject)
		b.WriteString("MIME-Version: 1.0\r\n")
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
		b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
		b.WriteString("\r\n")
		b.WriteString(body)
		b.WriteString("\r\n")
		return base64.RawURLEncoding.EncodeToString([]byte(b.String())), nil
	}
	boundary := fmt.Sprintf("jot-%d", time.Now().UTC().UnixNano())
	var b strings.Builder
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	b.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=%q\r\n", boundary)
	b.WriteString("\r\n")
	fmt.Fprintf(&b, "--%s\r\n", boundary)
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	b.WriteString("\r\n")
	for _, item := range attachmentPaths {
		data, err := os.ReadFile(item)
		if err != nil {
			return "", err
		}
		name := filepath.Base(item)
		mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
		if strings.TrimSpace(mimeType) == "" {
			mimeType = "application/octet-stream"
		}
		fmt.Fprintf(&b, "--%s\r\n", boundary)
		fmt.Fprintf(&b, "Content-Type: %s; name=%q\r\n", mimeType, name)
		b.WriteString("Content-Transfer-Encoding: base64\r\n")
		fmt.Fprintf(&b, "Content-Disposition: attachment; filename=%q\r\n", name)
		b.WriteString("\r\n")
		encoded := base64.StdEncoding.EncodeToString(data)
		for len(encoded) > 76 {
			b.WriteString(encoded[:76])
			b.WriteString("\r\n")
			encoded = encoded[76:]
		}
		if encoded != "" {
			b.WriteString(encoded)
			b.WriteString("\r\n")
		}
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return base64.RawURLEncoding.EncodeToString([]byte(b.String())), nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func gmailCountThreadAttachments(messages []NormalizedEmail) int {
	count := 0
	for _, message := range messages {
		count += len(message.Attachments)
	}
	return count
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func paramString(params map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			switch v := value.(type) {
			case string:
				return strings.TrimSpace(v)
			case fmt.Stringer:
				return strings.TrimSpace(v.String())
			case json.Number:
				return strings.TrimSpace(v.String())
			case float64:
				if v == float64(int64(v)) {
					return strconv.FormatInt(int64(v), 10)
				}
				return strconv.FormatFloat(v, 'f', -1, 64)
			case int:
				return strconv.Itoa(v)
			case int64:
				return strconv.FormatInt(v, 10)
			}
		}
	}
	return ""
}

func paramStringSlice(params map[string]any, keys ...string) []string {
	for _, key := range keys {
		value, ok := params[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case []string:
			return cloneAndTrimStrings(typed)
		case []any:
			out := make([]string, 0, len(typed))
			for _, item := range typed {
				if s := paramString(map[string]any{"value": item}, "value"); s != "" {
					out = append(out, s)
				}
			}
			return out
		case string:
			return splitAndTrimString(typed)
		default:
			if s := paramString(map[string]any{"value": typed}, "value"); s != "" {
				return splitAndTrimString(s)
			}
		}
	}
	return nil
}

func paramInt(params map[string]any, def int, keys ...string) int {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			switch v := value.(type) {
			case int:
				return v
			case int64:
				return int(v)
			case float64:
				return int(v)
			case json.Number:
				if n, err := v.Int64(); err == nil {
					return int(n)
				}
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					return n
				}
			}
		}
	}
	return def
}

func paramBool(params map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := params[key]; ok {
			switch v := value.(type) {
			case bool:
				return v
			case string:
				v = strings.TrimSpace(strings.ToLower(v))
				return v == "1" || v == "true" || v == "yes" || v == "on"
			case float64:
				return v != 0
			}
		}
	}
	return false
}

func mapNLToGmailQuery(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	if s == "" {
		return ""
	}
	sender := gmailQuerySenderTerm(s)
	attachment := strings.Contains(s, "attachment") || strings.Contains(s, "attachments")
	invoice := strings.Contains(s, "invoice")
	unread := strings.Contains(s, "unread")
	important := strings.Contains(s, "important")
	today := strings.Contains(s, "today")
	thisWeek := strings.Contains(s, "this week")
	thisMonth := strings.Contains(s, "this month")
	last30d := strings.Contains(s, "last 30") || strings.Contains(s, "30d") || strings.Contains(s, "30 days")

	parts := make([]string, 0, 6)
	if important {
		parts = append(parts, "is:important")
	}
	if unread {
		parts = append(parts, "is:unread")
	}
	if sender != "" {
		parts = append(parts, "from:"+sender)
	}
	if attachment {
		parts = append(parts, "has:attachment")
	}
	if invoice {
		parts = append(parts, "invoice")
	}
	switch {
	case today:
		parts = append(parts, "newer_than:1d")
	case thisWeek:
		parts = append(parts, "newer_than:7d")
	case thisMonth || last30d:
		parts = append(parts, "newer_than:30d")
	}
	if len(parts) == 0 {
		return strings.TrimSpace(input)
	}
	if unread && today {
		return "is:unread newer_than:1d"
	}
	if important && unread {
		return "is:important is:unread"
	}
	if important {
		return "is:important is:unread"
	}
	return strings.Join(parts, " ")
}

func gmailSearchQueryVariants(query string, intent gmailSearchIntent) []string {
	raw := strings.TrimSpace(query)
	variants := []string{raw}
	if clause := gmailSemanticQueryClause(raw); clause != "" && !strings.EqualFold(strings.TrimSpace(clause), raw) {
		variants = append(variants, clause)
	}
	if intent.ExactFact {
		if containsAnyTerm(intent.Terms, "passport") {
			variants = append(variants,
				"passport number",
				"passport application",
				"passport confirmation",
				"travel document",
				"identity document",
			)
		}
		if containsAnyTerm(intent.Terms, "visa") {
			variants = append(variants,
				"visa application",
				"travel document",
				"immigration",
				"entry clearance",
			)
		}
		if containsAnyTerm(intent.Terms, "brp", "biometric", "residence", "permit") {
			variants = append(variants,
				"biometric residence permit",
				"residence permit",
				"identity document",
			)
		}
		if containsAnyTerm(intent.Terms, "id", "identity") {
			variants = append(variants,
				"identity document",
				"government id",
				"id card",
			)
		}
	}
	return uniqueTrimmedStrings(variants)
}

func gmailSemanticQueryClause(s string) string {
	tokens := gmailSearchTokens(s)
	if len(tokens) == 0 {
		return ""
	}
	switch {
	case containsAnyTerm(tokens, "passport"):
		return `(passport OR "passport number" OR "passport no" OR "passport application" OR "passport confirmation" OR "travel document" OR "identity document")`
	case containsAnyTerm(tokens, "brp", "biometric", "residence", "permit"):
		return `(brp OR "biometric residence permit" OR "residence permit" OR "travel document" OR "identity document")`
	case containsAnyTerm(tokens, "visa"):
		return `(visa OR "visa application" OR immigration OR "entry clearance" OR "travel document")`
	case containsAnyTerm(tokens, "id", "identity"):
		return `(id OR identity OR "identity document" OR passport OR "government id" OR "id card")`
	default:
		return ""
	}
}

func gmailQuerySenderTerm(s string) string {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	for i := 0; i < len(words); i++ {
		if words[i] != "from" || i+1 >= len(words) {
			continue
		}
		var parts []string
		for j := i + 1; j < len(words); j++ {
			word := strings.TrimSpace(words[j])
			if gmailIsQueryBoundaryWord(word) {
				break
			}
			parts = append(parts, word)
			if strings.Contains(word, "@") {
				break
			}
		}
		if len(parts) == 0 {
			continue
		}
		if token := gmailNormalizeQueryToken(strings.Join(parts, " ")); token != "" {
			return token
		}
	}
	return ""
}

func gmailIsQueryBoundaryWord(word string) bool {
	switch strings.TrimSpace(word) {
	case "today", "this", "week", "month", "last", "newer", "older", "with", "has", "unread", "important", "attachment", "attachments", "invoice":
		return true
	default:
		return false
	}
}

func gmailNormalizeQueryToken(s string) string {
	s = strings.TrimSpace(strings.Trim(s, `"'`))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "@") {
		return s
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == ':' || r == '(' || r == ')' || r == '[' || r == ']'
	})
	if len(fields) == 0 {
		return ""
	}
	return strings.ToLower(fields[0])
}
