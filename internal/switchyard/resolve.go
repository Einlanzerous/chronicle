package switchyard

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxTicketBody bounds what one ticket answer may cost this process.
//
// IT IS NOT A DEFENSIVE ROUND NUMBER. `GET /v1/tickets/{key}` returns the FULL
// detail: loadTicketDetail (server/src/lib/tickets.ts:119) joins every
// non-plan-anchored comment, its author, and both levels of attachment into the
// payload. A card needs the title and the status; a busy ticket's answer is its
// entire conversation, and there is no query parameter on that route to ask for
// less -- the detail route takes `idOrKey` and nothing else.
//
// So a thirty-reference page is thirty full ticket threads arriving at a render
// and being discarded. Two megabytes is far above any real one (CHRN-49's own
// answer measured about nine kilobytes on 2026-09-12) and far below what a
// runaway answer could cost fifty times over inside one request.
const maxTicketBody = 2 << 20

// FetchTicket performs one GET /v1/tickets/{key} and returns WHAT CAME BACK,
// UNINTERPRETED.
//
// ============================================================================
// THIS IS THE ONE METHOD HERE THAT CAN SEE A TITLE AND A STATUS. IT RETURNS
// BYTES SO THAT IT CANNOT HAND ANYBODY A STRUCT TO ASSIGN INTO A COLUMN.
// ============================================================================
//
// The package comment has the whole of that argument. The short form: Ticket
// still carries a key and a URL and nothing else, internal/resolve owns the
// decode, and what it decodes into has no store path.
//
// IT DELIBERATELY DOES NOT GO THROUGH do(). do() turns every status >= 300 into
// an *Error and throws the body away, which is right for a create -- a refused
// create is a failure -- and wrong here, where a 404 is the ANSWER. Switchyard
// soft-deletes and resolveTicket excludes deleted rows, so "this ticket was
// deleted" arrives as a 404 and must reach the caller as a fact about the
// referent rather than as an outage. internal/resolve draws that line; this
// method's job is not to pre-empt it.
//
//	err != nil        no usable response at all -- nothing was answered.
//	err == nil        Switchyard answered, at `status`, with `body`.
//
// The deadline comes from ctx, which beats the client's own DefaultTimeout
// because do()'s requests and this one are both built with
// http.NewRequestWithContext. A render's budget is shorter than a triage
// batch's patience and sets its own.
func (c *Client) FetchTicket(ctx context.Context, key string) (status int, body []byte, err error) {
	if strings.TrimSpace(key) == "" {
		return 0, nil, fmt.Errorf("switchyard: no ticket key to resolve")
	}

	// ESCAPED, AND THE SAFETY DOES NOT REST ON THE GRAMMAR. The key here came
	// out of a note body, and internal/markdown makes the same argument about
	// the attributes it writes: reference.go admits only [A-Z][A-Z0-9]*-[0-9]+
	// today, and a later widening of that grammar must not silently become a
	// path traversal through this concatenation.
	path := "/v1/tickets/" + url.PathEscape(key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base.String()+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		// WRAPPED WITH %w so the cause survives. internal/resolve tells a
		// timeout from a refused dial with errors.As against net.Error, and the
		// two have different remedies on the card and in the log.
		//
		// The key is in the message and the token never is -- a credential in
		// an error is a credential in every aggregator that scrapes the logs.
		return 0, nil, fmt.Errorf("switchyard: GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxTicketBody+1))
	if err != nil {
		// A body that stopped arriving mid-read is no usable response, which is
		// what a transport error means here.
		return 0, nil, fmt.Errorf("switchyard: GET %s: read: %w", path, err)
	}
	if len(b) > maxTicketBody {
		// NOT AN ERROR, AND NOT A TRUNCATION EITHER.
		//
		// An error would say nobody answered, which is false and would stop the
		// rest of the page being checked. A truncated body would be handed on
		// as though it were the answer. Dropping it leaves exactly the true
		// claim: Switchyard answered and Chronicle could not use what it said,
		// which internal/resolve classifies as unreachable WITHOUT tripping,
		// because it predicts nothing about the next reference.
		return resp.StatusCode, nil, nil
	}
	return resp.StatusCode, b, nil
}
