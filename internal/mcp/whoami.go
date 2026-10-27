package mcp

import "context"

// WhoAmI is whoami's answer.
type WhoAmI struct {
	UserID      string `json:"user_id" jsonschema:"the account's id"`
	Email       string `json:"email" jsonschema:"the account's email"`
	DisplayName string `json:"display_name" jsonschema:"the account's display name"`
	Kind        string `json:"kind" jsonschema:"person or agent"`
	IsOwner     bool   `json:"is_owner" jsonschema:"whether this is the owner account"`
	Transport   string `json:"transport" jsonschema:"stdio or hosted: how this session reached Chronicle"`
	MayWrite    bool   `json:"may_write" jsonschema:"whether this session is offered tools that change anything"`
}

// whoami is the one tool CHRN-65 ships. It exists so the identity a session
// runs under can be checked end to end -- through the transport, the API
// client and a real handler -- before any tool that matters is written.
var whoami = define("whoami",
	"Report which Chronicle account this session acts as, whether it is a person or an agent, "+
		"how the session reached Chronicle (stdio or hosted), and whether it may write. "+
		"Reads the account live from Chronicle on every call and changes nothing. "+
		"Call it first when it matters whose name a later action will carry.",
	false,
	func(ctx context.Context, sess Session, _ struct{}) (WhoAmI, error) {
		// Asked of the API each time rather than read off the session's
		// Identity, so the answer is what Chronicle says now -- a renamed
		// account, or a credential that has since been revoked, shows.
		u, err := me(ctx, sess.API)
		if err != nil {
			return WhoAmI{}, err
		}
		return WhoAmI{
			UserID:      u.Id.String(),
			Email:       u.Email,
			DisplayName: u.DisplayName,
			Kind:        u.Kind,
			IsOwner:     u.IsOwner,
			Transport:   string(sess.Identity.Transport),
			MayWrite:    sess.Identity.MayWrite(),
		}, nil
	})
