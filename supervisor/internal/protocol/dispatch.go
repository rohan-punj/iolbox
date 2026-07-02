package protocol

import "encoding/json"

// Handler processes a single verb. args is the raw JSON of the request's "args"
// (may be nil). The returned value is marshaled into the response "result". A
// returned *Error is delivered as a protocol error response; any other error is
// wrapped as bad_request.
type Handler func(args json.RawMessage) (any, error)

// Dispatcher maps verbs to handlers.
type Dispatcher struct {
	handlers map[string]Handler
}

// NewDispatcher returns an empty dispatcher.
func NewDispatcher() *Dispatcher {
	return &Dispatcher{handlers: make(map[string]Handler)}
}

// Handle registers a handler for a verb, overwriting any previous one.
func (d *Dispatcher) Handle(op string, h Handler) {
	d.handlers[op] = h
}

// Dispatch invokes the handler for req.Op and builds a Response. It never
// returns an error; failures are encoded into the Response.
func (d *Dispatcher) Dispatch(req *Request) *Response {
	h, ok := d.handlers[req.Op]
	if !ok {
		return &Response{ID: req.ID, OK: false, Error: Errorf(CodeUnsupported, "unknown verb %q", req.Op)}
	}
	result, err := h(req.Args)
	if err != nil {
		perr, ok := err.(*Error)
		if !ok {
			perr = NewError(CodeBadRequest, err.Error())
		}
		return &Response{ID: req.ID, OK: false, Error: perr}
	}
	var raw json.RawMessage
	if result != nil {
		raw = mustMarshal(result)
	}
	return &Response{ID: req.ID, OK: true, Result: raw}
}

// Verbs returns the set of registered verb names (unordered).
func (d *Dispatcher) Verbs() []string {
	out := make([]string, 0, len(d.handlers))
	for k := range d.handlers {
		out = append(out, k)
	}
	return out
}
