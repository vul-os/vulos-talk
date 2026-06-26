package handlers

// testhelpers_test.go — shared request/response + in-memory storage helpers for
// the Vulos Talk handler test suites (meeting + spaces pentests). Carried and
// trimmed from office's files_authz_test.go / pentest_helpers_test.go: only the
// helpers the Talk tests actually use (memStorage, doReq, mustDecode, …) are
// kept; the office file/signing handler helpers were dropped with those routes.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vulos-talk/backend/models"
	"vulos-talk/backend/storage"

	"github.com/gin-gonic/gin"
)

// memStorage is a minimal in-memory Storage implementation for handler tests.
// It implements Meeting + Recording CRUD (the surfaces Talk routes); the
// remaining interface methods panic to surface any unintended use.
type memStorage struct {
	files      map[string]*models.File
	meetings   map[string]*models.Meeting
	recordings map[string]*models.MeetingRecording
}

func newMemStorage() *memStorage {
	return &memStorage{
		files:      make(map[string]*models.File),
		meetings:   make(map[string]*models.Meeting),
		recordings: make(map[string]*models.MeetingRecording),
	}
}

// --- File methods (unused by Talk routes, kept to satisfy the interface) ---
func (m *memStorage) ListFiles() ([]*models.File, error) {
	out := make([]*models.File, 0, len(m.files))
	for _, f := range m.files {
		out = append(out, f)
	}
	return out, nil
}
func (m *memStorage) GetFile(id string) (*models.File, error) {
	if f, ok := m.files[id]; ok {
		return f, nil
	}
	return nil, errFileNotFound
}
func (m *memStorage) CreateFile(f *models.File) error { m.files[f.ID] = f; return nil }
func (m *memStorage) UpdateFile(f *models.File) error {
	if _, ok := m.files[f.ID]; !ok {
		return errFileNotFound
	}
	m.files[f.ID] = f
	return nil
}
func (m *memStorage) DeleteFile(id string) error {
	if _, ok := m.files[id]; !ok {
		return errFileNotFound
	}
	delete(m.files, id)
	return nil
}

// --- unused interface methods (panic if hit) ---
func (m *memStorage) ListVersions(string) ([]*models.FileVersion, error)     { panic("unused") }
func (m *memStorage) GetVersion(string, string) (*models.FileVersion, error) { panic("unused") }
func (m *memStorage) CreateVersion(*models.FileVersion) error                { panic("unused") }
func (m *memStorage) PruneVersions(string, int) error                        { panic("unused") }
func (m *memStorage) LabelVersion(string, string, string) error              { panic("unused") }
func (m *memStorage) CreateEnvelope(*models.Envelope) error                  { panic("unused") }
func (m *memStorage) GetEnvelope(string) (*models.Envelope, error)           { panic("unused") }
func (m *memStorage) ListEnvelopes() ([]*models.Envelope, error)             { panic("unused") }
func (m *memStorage) UpdateEnvelope(*models.Envelope) error                  { panic("unused") }
func (m *memStorage) DeleteEnvelope(string) error                            { panic("unused") }
func (m *memStorage) UpsertSigner(*models.Signer) error                      { panic("unused") }
func (m *memStorage) GetSigner(string) (*models.Signer, error)               { panic("unused") }
func (m *memStorage) ListSignersByEnvelope(string) ([]*models.Signer, error) { panic("unused") }
func (m *memStorage) AppendAuditEvent(*models.AuditEvent) error              { panic("unused") }
func (m *memStorage) ListAuditEvents(string) ([]*models.AuditEvent, error)   { panic("unused") }
func (m *memStorage) StoreSignerToken(string, string, string) error          { panic("unused") }
func (m *memStorage) ResolveToken(string) (string, string, error)            { panic("unused") }
func (m *memStorage) StoreSealedPDF(string, []byte) error                    { panic("unused") }
func (m *memStorage) GetSealedPDF(string) ([]byte, error)                    { panic("unused") }
func (m *memStorage) CreateComment(*models.Comment) error                    { panic("unused") }
func (m *memStorage) GetComment(string, string) (*models.Comment, error)     { panic("unused") }
func (m *memStorage) ListComments(string) ([]*models.Comment, error)         { panic("unused") }
func (m *memStorage) UpdateComment(*models.Comment) error                    { panic("unused") }
func (m *memStorage) DeleteComment(string, string) error                     { panic("unused") }
func (m *memStorage) CreateReply(*models.CommentReply) error                 { panic("unused") }
func (m *memStorage) GetReply(string, string) (*models.CommentReply, error)  { panic("unused") }
func (m *memStorage) ListReplies(string) ([]*models.CommentReply, error)     { panic("unused") }
func (m *memStorage) UpdateReply(*models.CommentReply) error                 { panic("unused") }
func (m *memStorage) CreateMeeting(mt *models.Meeting) error {
	m.meetings[mt.ID] = mt
	return nil
}
func (m *memStorage) GetMeeting(id string) (*models.Meeting, error) {
	mt, ok := m.meetings[id]
	if !ok {
		return nil, errFile("meeting not found")
	}
	return mt, nil
}
func (m *memStorage) ListMeetings() ([]*models.Meeting, error) {
	out := make([]*models.Meeting, 0, len(m.meetings))
	for _, mt := range m.meetings {
		out = append(out, mt)
	}
	return out, nil
}
func (m *memStorage) UpdateMeeting(mt *models.Meeting) error {
	if _, ok := m.meetings[mt.ID]; !ok {
		return errFile("meeting not found")
	}
	m.meetings[mt.ID] = mt
	return nil
}
func (m *memStorage) DeleteMeeting(id string) error {
	if _, ok := m.meetings[id]; !ok {
		return errFile("meeting not found")
	}
	delete(m.meetings, id)
	return nil
}
func (m *memStorage) CreateSuggestion(*models.Suggestion) error                { panic("unused") }
func (m *memStorage) GetSuggestion(string, string) (*models.Suggestion, error) { panic("unused") }
func (m *memStorage) ListSuggestions(string) ([]*models.Suggestion, error)     { panic("unused") }
func (m *memStorage) UpdateSuggestion(*models.Suggestion) error                { panic("unused") }
func (m *memStorage) DeleteSuggestion(string, string) error                    { panic("unused") }
func (m *memStorage) CreateRecording(r *models.MeetingRecording) error {
	m.recordings[r.ID] = r
	return nil
}
func (m *memStorage) ListRecordings(roomID string) ([]*models.MeetingRecording, error) {
	var out []*models.MeetingRecording
	for _, r := range m.recordings {
		if r.RoomID == roomID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memStorage) GetRecording(id string) (*models.MeetingRecording, error) {
	if r, ok := m.recordings[id]; ok {
		return r, nil
	}
	return nil, errFile("recording not found")
}
func (m *memStorage) DeleteRecording(id string) error {
	delete(m.recordings, id)
	return nil
}

var _ storage.Storage = (*memStorage)(nil)

var errFileNotFound = errFile("file not found")

type errFile string

func (e errFile) Error() string { return string(e) }

// doReq serves a JSON request through the router and returns the recorder.
func doReq(r *gin.Engine, method, path string, body interface{}) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// newRecorder is a thin alias kept local to the pentest suites.
func newRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

// newReqWithHeader builds a request carrying a forged X-Account-ID header.
func newReqWithHeader(method, path, forgedAccountID string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if forgedAccountID != "" {
		req.Header.Set("X-Account-ID", forgedAccountID)
	}
	return req
}

// mustDecode unmarshals a recorder body into v, failing the test on error.
func mustDecode(t *testing.T, w *httptest.ResponseRecorder, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, w.Body.String())
	}
}
