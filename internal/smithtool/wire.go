package smithtool

import "encoding/json"

// request is what a client sends: a call of a tool, or a listing.
type request struct {
	Op   string   `json:"op"`
	Name string   `json:"name,omitempty"`
	Args []string `json:"args,omitempty"`
}

// The ops a request carries.
const (
	opCall = "call"
	opList = "list"
)

// frame is one line of a reply: streamed output, or the final status.
type frame struct {
	Out    []byte          `json:"out,omitempty"`
	Done   bool            `json:"done,omitempty"`
	Status int             `json:"status,omitempty"`
	Error  string          `json:"error,omitempty"`
	Tools  json.RawMessage `json:"tools,omitempty"`
}

// The final statuses a reply carries.
const (
	statusOK      = 0
	statusFailed  = 1
	statusRefused = 2
)
