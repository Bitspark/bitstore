package bitstore

// The types exchanged over the HTTP bindings (api/HTTP.md and
// api/management/HTTP.md). api/schema/ and api/management/schema/ are
// generated from them by `go generate ./api`, and a drift test holds the
// committed schemas to a fresh generation. A ${contract} or ${surface} in a
// schema tag is the value of the surface being generated.

// NamesRequest is the body of POST /v1/stat and POST /v1/get.
type NamesRequest struct {
	Names []Name `json:"names"`
}

// StatResponse answers POST /v1/stat: a size for every requested name that is
// here, and no entry for one that is not.
type StatResponse struct {
	Sizes map[Name]int64 `json:"sizes" schema:"minimum=0"`
}

// PutResponse answers POST /v1/blobs.
type PutResponse struct {
	Name Name  `json:"name"`
	Size int64 `json:"size" schema:"minimum=0"`
}

// PutManyResponse answers POST /v1/put, one item per record in request order.
type PutManyResponse struct {
	Results []PutManyItem `json:"results"`
}

// PutManyItem is one record's outcome: its name and size, or the error that
// refused it alone.
type PutManyItem struct {
	Name    Name   `json:"name,omitempty"`
	Size    *int64 `json:"size,omitempty"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// JSONSchema is PutManyItem's schema: exactly one of the two outcomes.
func (PutManyItem) JSONSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"oneOf": []any{
			map[string]any{
				"required":             []any{"name", "size"},
				"additionalProperties": false,
				"properties": map[string]any{
					"name": nameSchema(),
					"size": map[string]any{"type": "integer", "minimum": 0},
				},
			},
			map[string]any{
				"required":             []any{"error", "message"},
				"additionalProperties": false,
				"properties": map[string]any{
					"error":   map[string]any{"type": "string", "pattern": codePattern},
					"message": map[string]any{"type": "string", "minLength": 1},
				},
			},
		},
	}
}

// Error is the envelope of every HTTP error: a code the client decides on and
// a message for people.
type Error struct {
	Code    string         `json:"error" schema:"pattern=^[a-z][a-z0-9]*(_[a-z0-9]+)*$"`
	Message string         `json:"message" schema:"minLength=1"`
	Details map[string]any `json:"details,omitempty"`
}

// Health is the envelope of GET /livez and GET /healthz.
type Health struct {
	Status   string  `json:"status" schema:"enum=ok|failing"`
	Service  string  `json:"service" schema:"const=bitstore"`
	Contract string  `json:"contract" schema:"const=${contract}"`
	Build    Build   `json:"build"`
	Time     string  `json:"time" schema:"pattern=^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}([.][0-9]+)?Z$"`
	Checks   []Check `json:"checks"`
}

// Build names the source an executable was built from.
type Build struct {
	Revision string `json:"revision" schema:"pattern=^([0-9a-f]{40}|[0-9a-f]{64}|unknown)$"`
	Dirty    bool   `json:"dirty"`
	Go       string `json:"go" schema:"minLength=1"`
}

// Check is one dependency's state in the health envelope.
type Check struct {
	Name   string `json:"name" schema:"minLength=1"`
	Status string `json:"status" schema:"enum=ok|failing"`
	Detail string `json:"detail"`
}

// Describe is the document of GET /describe.
type Describe struct {
	Service    string           `json:"service" schema:"const=bitstore"`
	Surface    string           `json:"surface,omitempty" schema:"const=${surface}"`
	Contract   string           `json:"contract" schema:"const=${contract}"`
	Build      Build            `json:"build"`
	Bindings   Bindings         `json:"bindings"`
	Operations []Operation      `json:"operations"`
	Refusals   []Refusal        `json:"refusals"`
	Files      []string         `json:"files"`
	Limits     Limits           `json:"limits"`
	Profiles   []BackendProfile `json:"profiles,omitempty"`
}

// Bindings names the primary binding and describes each binding offered.
type Bindings struct {
	Primary string      `json:"primary" schema:"const=http"`
	HTTP    BindingHTTP `json:"http"`
}

// BindingHTTP is the HTTP binding's native application prefix.
type BindingHTTP struct {
	Prefix string `json:"prefix" schema:"const=/v1"`
}

// Limits is what the instance says beyond the envelope.
type Limits struct {
	// MaxBlobBytes is the per-blob cap, or 0 for none.
	MaxBlobBytes int64 `json:"max_blob_bytes" schema:"minimum=0"`
}

// Operation is a row of the operation table, api/operations.json.
type Operation struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind" schema:"enum=read|mutation|stream|operational"`
	Doc      string   `json:"doc"`
	Params   []Param  `json:"params,omitempty"`
	Request  string   `json:"request,omitempty"`
	Response string   `json:"response"`
	Errors   []string `json:"errors"`
	HTTP     Route    `json:"http"`
}

// Param is an operation's scalar input.
type Param struct {
	Name     string `json:"name"`
	Type     string `json:"type" schema:"enum=string|integer|boolean"`
	Required bool   `json:"required"`
	Repeated bool   `json:"repeated,omitempty"`
	Doc      string `json:"doc"`
}

// Route is an operation's HTTP method and path template.
type Route struct {
	Method string `json:"method" schema:"enum=GET|POST|PUT|PATCH|DELETE"`
	Path   string `json:"path"`
}

// Refusal is a row of the refusal table.
type Refusal struct {
	Name    string `json:"name"`
	HTTP    int    `json:"http"`
	Meaning string `json:"meaning"`
}

const codePattern = `^[a-z][a-z0-9]*(_[a-z0-9]+)*$`

// nameSchema is the schema of a Name.
func nameSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": grammar.String()}
}

// JSONSchema is a Name's schema: the name grammar.
func (Name) JSONSchema() map[string]any { return nameSchema() }
