package switchyard

import (
	"context"
	"fmt"
	"net/url"
)

// Project is a live destination, as Switchyard describes it.
//
// Three fields, because three is what a router needs: the key it must answer
// with, and the name and description that let a model tell which project a memo
// about "the tablet in the kitchen" belongs to. Nothing here is stored.
type Project struct {
	Key         string
	Name        string
	Description string
}

type projectsPage struct {
	Items []struct {
		Key         string  `json:"key"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ArchivedAt  *string `json:"archived_at"`
		DeletedAt   *string `json:"deleted_at"`
	} `json:"items"`
	Page struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	} `json:"page"`
}

// Projects returns every LIVE project, following pagination.
//
// PAGINATION IS FOLLOWED RATHER THAN ASSUMED AWAY. A caller that stopped at the
// first page would be a router unable to reach the projects at the end of the
// list — the same invisible failure a stale catalogue causes, arriving through
// a different door, and just as hard to see from a proposal that looks fine.
//
// ARCHIVED AND DELETED PROJECTS ARE EXCLUDED. The endpoint omits them unless
// asked, and anything carrying archived_at or deleted_at is dropped here anyway.
// A project nobody wants new tickets in must not be offered as a destination,
// and the eval set carries a fixture about exactly that bug in another service.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	var out []Project
	seen := map[string]bool{}
	cursor := ""

	for page := 0; ; page++ {
		if page > 100 {
			return nil, fmt.Errorf("switchyard: /v1/projects did not stop paginating")
		}
		path := "/v1/projects"
		if cursor != "" {
			path += "?" + url.Values{"cursor": {cursor}}.Encode()
		}
		var body projectsPage
		if err := c.do(ctx, "GET", path, nil, nil, &body); err != nil {
			return nil, err
		}
		for _, p := range body.Items {
			if p.ArchivedAt != nil || p.DeletedAt != nil || p.Key == "" || p.Name == "" || seen[p.Key] {
				continue
			}
			seen[p.Key] = true
			out = append(out, Project{Key: p.Key, Name: p.Name, Description: p.Description})
		}
		if !body.Page.HasMore || body.Page.NextCursor == "" {
			return out, nil
		}
		cursor = body.Page.NextCursor
	}
}
