package handlers

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vul-os/vulos-apps/appsplatform"
	"github.com/vul-os/vulos-apps/mcp"
)

// TestMCPToolsListExposesTalkTools mounts Talk's MCP handler over the SAME
// TalkAdapter + registry the REST runtime uses, then drives initialize →
// tools/list with a scoped app token and asserts Talk's Act actions show up as
// MCP tools. This is the MCP analogue of the bot-API surface test.
func TestMCPToolsListExposesTalkTools(t *testing.T) {
	sp := testHandler(t)
	reg := appsplatform.NewMemoryRegistry()
	disp := appsplatform.NewDispatcher(reg, appsplatform.ProductTalk)
	adapter := NewTalkAdapter(sp)

	h, err := mcp.NewHandler(mcp.MCPConfig{
		Adapter:  adapter,
		Registry: reg,
		Emit:     disp.EmitFunc(),
	})
	if err != nil {
		t.Fatalf("mcp.NewHandler: %v", err)
	}

	created, err := reg.Create(appsplatform.CreateParams{
		Name:     "agent",
		OwnerID:  "alice",
		Products: []string{appsplatform.ProductTalk},
		Scopes:   []string{appsplatform.ScopeAppsRead, appsplatform.ScopeAppsWrite},
	})
	if err != nil {
		t.Fatalf("create app: %v", err)
	}

	call := func(method string, params any) mcp.Response {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": method, "params": params,
		})
		req := httptest.NewRequest("POST", "/mcp", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+created.Token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		var resp mcp.Response
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode %s response: %v (body=%s)", method, err, w.Body.String())
		}
		return resp
	}

	if init := call("initialize", map[string]any{"protocolVersion": mcp.ProtocolVersion}); init.Error != nil {
		t.Fatalf("initialize error: %+v", init.Error)
	}

	tools := call("tools/list", nil)
	if tools.Error != nil {
		t.Fatalf("tools/list error: %+v", tools.Error)
	}
	got, _ := json.Marshal(tools.Result)
	for _, want := range []string{ActMessagePost, ActReactionAdd, ActReactionRemove} {
		if !strings.Contains(string(got), want) {
			t.Errorf("tools/list missing tool %q; got %s", want, got)
		}
	}

	resources := call("resources/list", nil)
	if resources.Error != nil {
		t.Fatalf("resources/list error: %+v", resources.Error)
	}
	res, _ := json.Marshal(resources.Result)
	for _, want := range []string{ReadChannels, ReadHistory, ReadMembers} {
		if !strings.Contains(string(res), want) {
			t.Errorf("resources/list missing kind %q; got %s", want, res)
		}
	}
}
