package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The write gate, before any real write tool exists: a fixture that Mutates is
// offered to a stdio session on an agent account and to nobody else.
func TestBuildServerOffersMutatingToolsOnlyToAnAgentOnStdio(t *testing.T) {
	fixture := define("fixture_write", "A tool that claims to change something.", true,
		func(context.Context, Session, struct{}) (struct{}, error) { return struct{}{}, nil })
	tools := []Tool{whoami, fixture}

	for _, tc := range []struct {
		name      string
		id        Identity
		wantWrite bool
	}{
		{"hosted, person", Identity{Transport: TransportHosted, Kind: KindPerson}, false},
		// Cannot arise -- the exchange refuses an agent -- and is refused here
		// too, because the gate must not depend on that.
		{"hosted, agent", Identity{Transport: TransportHosted, Kind: KindAgent}, false},
		{"stdio, person", Identity{Transport: TransportStdio, Kind: KindPerson}, false},
		{"stdio, agent", Identity{Transport: TransportStdio, Kind: KindAgent}, true},
		{"an unknown transport", Identity{Transport: "carrier-pigeon", Kind: KindAgent}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.MayWrite(); got != tc.wantWrite {
				t.Fatalf("MayWrite = %v, want %v", got, tc.wantWrite)
			}
			listed := listTools(t, buildServer(Build{}, Session{Identity: tc.id}, nil, tools))

			if _, ok := listed["whoami"]; !ok {
				t.Error("whoami is not offered; a read tool is offered to every session")
			}
			_, gotWrite := listed["fixture_write"]
			if gotWrite != tc.wantWrite {
				t.Errorf("fixture_write offered = %v, want %v", gotWrite, tc.wantWrite)
			}
			// The hint is derived from Mutates, so the two cannot disagree.
			for name, tool := range listed {
				wantReadOnly := name != "fixture_write"
				if tool.Annotations == nil || tool.Annotations.ReadOnlyHint != wantReadOnly {
					t.Errorf("%s readOnlyHint = %+v, want %v", name, tool.Annotations, wantReadOnly)
				}
			}
		})
	}
}

// The shipped registry has no tool that changes anything. CHRN-67 is the
// ticket that makes this test wrong, and it should have to say so.
func TestTheShippedRegistryIsReadOnly(t *testing.T) {
	for _, tool := range Tools() {
		if tool.Mutates {
			t.Errorf("%s declares Mutates; CHRN-65 ships no write tool", tool.Name)
		}
	}
}

func listTools(t *testing.T, server *sdk.Server) map[string]*sdk.Tool {
	t.Helper()
	ctx := context.Background()
	serverSide, clientSide := sdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, serverSide, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "0"}, nil).Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	out := map[string]*sdk.Tool{}
	for _, tool := range res.Tools {
		out[tool.Name] = tool
	}
	return out
}
