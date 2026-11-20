package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/spf13/cobra"
	"github.com/wojons/muster/pkg/generator"
	"github.com/wojons/muster/pkg/openapi"

	"github.com/totalwindupflightsystems/musterflow/internal/specfix"
)

// Spec plans are the persisted, derived form of a connected API's document.
//
// PERF-002: before this cache existed every CLI operation re-read the source
// spec, re-ran the tolerance repair pass, and re-parsed the whole document
// (measurably ~1.0-1.4 s of parse for a 4.3 MB spec, plus one HTTP GET per call
// for URL-connected APIs). The plan holds exactly what command generation
// consumes — the extracted operations plus the connection metadata — so an
// operation call rebuilds the command tree without touching the source spec or
// its URL.
//
// The plan is stored per RESOURCE as well as per connection: an operation call
// reads the small index plus the one resource file it invokes, so its cost is
// tied to the invoked resource, not to how many operations the API declares.
//
// DF-048: because the plan IS what the loader consumes, `connect` can build the
// command tree from it before reporting success. A spec that parses but yields
// no callable commands can no longer be reported as connected.
const (
	// planFormatVersion guards the on-disk shape. A plan written by an older
	// build is treated as a cache miss (the source is re-read once) instead of
	// being decoded into a newer struct with different semantics.
	planFormatVersion = 1

	// planDirName is the directory under the data dir holding one plan
	// directory per connected API. Plans live in files (not the DuckDB
	// registry) so an operation call never needs write access to the store the
	// dashboard may hold a lock on.
	planDirName = "specs"

	// planIndexFile names the metadata + resource index inside a plan dir.
	planIndexFile = "index.json"
)

// SpecPlan is the in-memory command plan for one connected API: everything
// command generation needs, and nothing that requires re-parsing the spec.
type SpecPlan struct {
	Format int `json:"format"`
	// ConnectionID is the registry id this plan belongs to. It also names the
	// cache directory, so a re-connect of the same spec replaces in place.
	ConnectionID string `json:"connection_id"`
	// SpecURL is the source the plan was derived from (file path or URL).
	SpecURL string `json:"spec_url"`
	// ContentSHA256 fingerprints the tolerant-normalized bytes that were
	// parsed. A re-connect rewrites the plan; the hash lets a reader tell a
	// stale plan from a current one without reading the source back.
	ContentSHA256 string    `json:"content_sha256"`
	SpecVersion   string    `json:"spec_version"`
	Title         string    `json:"title"`
	BaseURL       string    `json:"base_url"`
	EndpointCount int       `json:"endpoint_count"`
	CreatedAt     time.Time `json:"created_at"`

	// Tolerated and Repaired record what the tolerance pass did, so a plan
	// loaded long after connect can still say the connection is degraded.
	Tolerated []specfix.Warning `json:"tolerated,omitempty"`
	Repaired  bool              `json:"repaired,omitempty"`

	// Operations is the generator's input: one entry per callable operation.
	Operations []*openapi.Operation `json:"operations"`
	// Resources indexes Operations by the resource group the generator puts
	// them in (indices into Operations). Empty when the index could not be
	// derived completely; the plan is then stored whole.
	Resources []PlanResource `json:"-"`
}

// PlanResource is one resource group in a plan's command index: the group
// command name the generator produces and the plan operations that belong to
// it. The index is derived from the generator's OWN output (cobra group names
// plus the path/method annotations stamped on every generated leaf), so it can
// never disagree with the command tree about which operation lives where.
type PlanResource struct {
	Name string `json:"name"`
	// Short is the group command's one-line description.
	Short string `json:"short"`
	// Operations indexes into SpecPlan.Operations.
	Operations []int `json:"operations"`
}

// BuildPlan derives the plan from a parsed document. It applies the same
// document preparation the per-call loader used to apply right before
// generation (servers cleared so the connection's resolved base URL wins), so
// the plan reproduces the exact command tree the old path built.
func BuildPlan(doc *openapi3.T, tolerated []specfix.Warning, repaired bool) (*SpecPlan, error) {
	ClearDocumentServers(doc)

	ops, err := openapi.NewParser().ExtractOperations(doc)
	if err != nil {
		return nil, fmt.Errorf("extract operations: %w", err)
	}

	plan := &SpecPlan{
		Format:        planFormatVersion,
		EndpointCount: countEndpoints(doc),
		Tolerated:     tolerated,
		Repaired:      repaired,
		Operations:    ops,
		CreatedAt:     time.Now().UTC(),
	}
	if doc.Info != nil {
		plan.SpecVersion = doc.Info.Version
	}
	if len(doc.Servers) > 0 && doc.Servers[0] != nil {
		plan.BaseURL = doc.Servers[0].URL
	}
	return plan, nil
}

// ClearDocumentServers drops root, path-item and per-operation server URLs so
// generated commands fall back to the generator config's BaseURL (the fully
// resolved base URL from the connection). Many specs declare relative server
// URLs like "/api/v3", which cannot be used as a request base.
func ClearDocumentServers(doc *openapi3.T) {
	if doc == nil {
		return
	}
	doc.Servers = nil
	if doc.Paths == nil {
		return
	}
	for _, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		pathItem.Servers = nil
		for _, op := range []*openapi3.Operation{
			pathItem.Get, pathItem.Post, pathItem.Put, pathItem.Patch,
			pathItem.Delete, pathItem.Head, pathItem.Options, pathItem.Trace,
		} {
			if op != nil {
				op.Servers = nil
			}
		}
	}
}

// SynthesizeDocument rebuilds the minimal OpenAPI document the generator needs
// from a plan. The generator consumes operation metadata only, which is what
// the plan carries; rebuilding the document from it costs milliseconds instead
// of the ~0.5-1.4 s a full spec parse takes on a multi-megabyte document.
//
// The reconstruction is exact for everything command generation reads:
// operation id/tags/summary/description, merged parameters (name, location,
// required, type, enum, default, example, description), JSON request-body
// properties (name, type, description, required, enum, default) and multipart
// detection.
func SynthesizeDocument(plan *SpecPlan) *openapi3.T {
	doc := &openapi3.T{
		OpenAPI: "3.1.0",
		Info:    &openapi3.Info{Title: plan.Title, Version: plan.SpecVersion},
		Paths:   openapi3.NewPaths(),
	}

	byPath := make(map[string]*openapi3.PathItem, len(plan.Operations))
	for _, op := range plan.Operations {
		if op == nil || op.Path == "" || op.Method == "" {
			continue
		}
		pathItem := byPath[op.Path]
		if pathItem == nil {
			pathItem = &openapi3.PathItem{}
			byPath[op.Path] = pathItem
			doc.Paths.Set(op.Path, pathItem)
		}

		synth := &openapi3.Operation{
			OperationID: op.ID,
			Tags:        op.Tags,
			Summary:     op.Summary,
			Description: op.Description,
		}
		for _, p := range op.Parameters {
			synth.Parameters = append(synth.Parameters, synthesizeParameter(p))
		}
		synth.RequestBody = synthesizeRequestBody(op)

		switch strings.ToUpper(op.Method) {
		case "GET":
			pathItem.Get = synth
		case "POST":
			pathItem.Post = synth
		case "PUT":
			pathItem.Put = synth
		case "PATCH":
			pathItem.Patch = synth
		case "DELETE":
			pathItem.Delete = synth
		case "HEAD":
			pathItem.Head = synth
		case "OPTIONS":
			pathItem.Options = synth
		case "TRACE":
			pathItem.Trace = synth
		}
	}
	return doc
}

func synthesizeParameter(p openapi.Parameter) *openapi3.ParameterRef {
	param := &openapi3.Parameter{
		Name:        p.Name,
		In:          p.In,
		Required:    p.Required,
		Description: p.Description,
	}
	schema := &openapi3.Schema{Enum: p.Enum, Default: p.Default, Example: p.Example}
	if p.Type != "" {
		schema.Type = &openapi3.Types{p.Type}
	}
	param.Schema = &openapi3.SchemaRef{Value: schema}
	return &openapi3.ParameterRef{Value: param}
}

// synthesizeRequestBody rebuilds the JSON (and, when the plan says so,
// multipart) request body from the extracted properties. A plan with no body
// properties yields no request body, exactly as extraction reported.
func synthesizeRequestBody(op *openapi.Operation) *openapi3.RequestBodyRef {
	if len(op.RequestBody) == 0 && len(op.MultipartFields) == 0 {
		return nil
	}
	content := openapi3.Content{}
	if len(op.RequestBody) > 0 {
		content["application/json"] = &openapi3.MediaType{Schema: schemaFromProps(op.RequestBody)}
	}
	if op.HasMultiPart && len(op.MultipartFields) > 0 {
		content["multipart/form-data"] = &openapi3.MediaType{Schema: schemaFromProps(op.MultipartFields)}
	}
	if len(content) == 0 {
		return nil
	}
	return &openapi3.RequestBodyRef{Value: &openapi3.RequestBody{Content: content}}
}

func schemaFromProps(props []openapi.RequestBodyProperty) *openapi3.SchemaRef {
	properties := make(map[string]*openapi3.SchemaRef, len(props))
	var required []string
	for _, p := range props {
		schema := &openapi3.Schema{Description: p.Description, Enum: p.Enum, Default: p.Default}
		if p.Type != "" {
			schema.Type = &openapi3.Types{p.Type}
		}
		properties[p.Name] = &openapi3.SchemaRef{Value: schema}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	sort.Strings(required)
	return &openapi3.SchemaRef{Value: &openapi3.Schema{
		Type:       &openapi3.Types{"object"},
		Required:   required,
		Properties: properties,
	}}
}

// IndexPlanResources records which operations belong to which resource group by
// walking a generated command tree. It must be called with the commands
// produced by CommandsFromPlan for the same plan.
//
// A leaf without the generator's path/method annotations (or one that matches
// no planned operation) makes the index incomplete, and the index is then left
// empty: callers fall back to reading/building the whole plan, which is correct
// but not per-resource. Silently indexing a subset would be worse — an
// operation would become unreachable.
func IndexPlanResources(plan *SpecPlan, commands []*cobra.Command) {
	if plan == nil {
		return
	}
	type opKey struct{ path, method string }
	byKey := make(map[opKey]int, len(plan.Operations))
	for i, op := range plan.Operations {
		if op == nil {
			continue
		}
		byKey[opKey{op.Path, strings.ToUpper(op.Method)}] = i
	}

	index := make([]PlanResource, 0, len(commands))
	seen := make(map[int]bool, len(plan.Operations))
	for _, group := range commands {
		if group == nil {
			continue
		}
		res := PlanResource{Name: group.Name(), Short: group.Short}
		for _, leaf := range group.Commands() {
			if leaf == nil || leaf.Annotations == nil {
				plan.Resources = nil
				return
			}
			idx, ok := byKey[opKey{
				leaf.Annotations[generator.AnnotationOperationPath],
				strings.ToUpper(leaf.Annotations[generator.AnnotationOperationMethod]),
			}]
			if !ok {
				plan.Resources = nil
				return
			}
			res.Operations = append(res.Operations, idx)
			seen[idx] = true
		}
		sort.Ints(res.Operations)
		index = append(index, res)
	}

	// Every planned operation must be reachable through some group; otherwise
	// the plan carries an operation the tree cannot run.
	if len(seen) != len(plan.Operations) {
		plan.Resources = nil
		return
	}
	sort.Slice(index, func(i, j int) bool { return index[i].Name < index[j].Name })
	plan.Resources = index
}

// CommandsFromPlan builds the cobra command tree for a connected API from its
// plan. This is the ONE command-tree path: `connect` calls it to prove the spec
// it is about to report as connected actually loads, `refresh` calls it to
// validate the refreshed document, and every CLI operation call builds its
// commands through it. A defect that blocks loading therefore cannot be
// reported as a successful connect (DF-048).
func CommandsFromPlan(plan *SpecPlan, conn *APIConnection) ([]*cobra.Command, error) {
	if plan == nil {
		return nil, fmt.Errorf("no command plan")
	}

	genCfg := GenerateCommandConfig(conn)
	if genCfg.BaseURL == "" {
		genCfg.BaseURL = plan.BaseURL
	}
	gen, err := generator.NewGenerator(*genCfg)
	if err != nil {
		return nil, fmt.Errorf("create generator: %w", err)
	}

	commands, err := gen.GenerateCommands(SynthesizeDocument(plan))
	if err != nil {
		return nil, fmt.Errorf("generate commands: %w", err)
	}
	return commands, nil
}

// ContentHash fingerprints spec bytes for the plan's content_sha256 field.
func ContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

// PlanDir is the directory holding one connection's cached plan.
func PlanDir(dataDir, connID string) string {
	return filepath.Join(dataDir, planDirName, connID)
}

// planIndexDoc is the on-disk index: connection metadata, the operation store
// file, and one entry per resource pointing at the byte ranges its operations
// occupy in that file.
type planIndexDoc struct {
	Format        int               `json:"format"`
	ConnectionID  string            `json:"connection_id"`
	SpecURL       string            `json:"spec_url"`
	ContentSHA256 string            `json:"content_sha256"`
	SpecVersion   string            `json:"spec_version"`
	Title         string            `json:"title"`
	BaseURL       string            `json:"base_url"`
	EndpointCount int               `json:"endpoint_count"`
	CreatedAt     time.Time         `json:"created_at"`
	Tolerated     []specfix.Warning `json:"tolerated,omitempty"`
	Repaired      bool              `json:"repaired,omitempty"`
	OpsFile       string            `json:"ops_file"`
	Resources     []planResourceDoc `json:"resources,omitempty"`
}

// planResourceDoc is one resource entry in the on-disk index: its command name,
// description and the byte ranges of its operations in the ops file.
type planResourceDoc struct {
	Name  string      `json:"name"`
	Short string      `json:"short"`
	Refs  []planOpRef `json:"refs"`
}

// planOpRef locates one operation's JSON inside the plan's ops file. Byte
// ranges (not line numbers) let an operation call read exactly the operations
// it needs, without parsing the rest of a large API.
type planOpRef struct {
	Offset int64 `json:"o"`
	Length int   `json:"l"`
}

// PlanIndex is a connection's cached plan metadata plus its resource index. It
// is what an operation call reads before loading just the resource it invokes.
type PlanIndex struct {
	doc planIndexDoc
}

// Resources returns the indexed resource group names with their descriptions,
// in the order the plan stored them.
func (p *PlanIndex) Resources() []PlanResource {
	if p == nil {
		return nil
	}
	out := make([]PlanResource, 0, len(p.doc.Resources))
	for _, r := range p.doc.Resources {
		out = append(out, PlanResource{Name: r.Name, Short: r.Short})
	}
	return out
}

// HasResource reports whether the plan indexes the named resource.
func (p *PlanIndex) HasResource(name string) bool {
	if p == nil {
		return false
	}
	for _, r := range p.doc.Resources {
		if r.Name == name {
			return true
		}
	}
	return false
}

// SpecURL returns the source the cached plan was derived from.
func (p *PlanIndex) SpecURL() string {
	if p == nil {
		return ""
	}
	return p.doc.SpecURL
}

// Plan rebuilds an in-memory plan around a set of operations. The caller
// supplies the operations (one resource's, or all of them) that were read from
// the cache.
func (p *PlanIndex) Plan(ops []*openapi.Operation) *SpecPlan {
	if p == nil {
		return nil
	}
	return &SpecPlan{
		Format:        p.doc.Format,
		ConnectionID:  p.doc.ConnectionID,
		SpecURL:       p.doc.SpecURL,
		ContentSHA256: p.doc.ContentSHA256,
		SpecVersion:   p.doc.SpecVersion,
		Title:         p.doc.Title,
		BaseURL:       p.doc.BaseURL,
		EndpointCount: p.doc.EndpointCount,
		CreatedAt:     p.doc.CreatedAt,
		Tolerated:     p.doc.Tolerated,
		Repaired:      p.doc.Repaired,
		Operations:    ops,
	}
}

// ErrNoPlan reports that a connection has no usable cached plan (never written,
// removed, or written by an incompatible build). Callers fall back to the
// source spec once and rewrite the cache.
var ErrNoPlan = fmt.Errorf("no cached spec plan")

// WritePlan persists a plan: every operation is written once to an ops file
// (newline-delimited JSON), the index records the byte range of each resource's
// operations, and each file is published atomically (temp file + rename). A
// concurrent reader therefore never observes a half-written plan: it either
// sees the previous index or the new one.
func WritePlan(dataDir string, plan *SpecPlan) error {
	if plan == nil || plan.ConnectionID == "" {
		return fmt.Errorf("write plan: missing connection id")
	}
	plan.Format = planFormatVersion

	dir := PlanDir(dataDir, plan.ConnectionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create plan dir: %w", err)
	}

	// Lay the operations out once, remembering each one's byte range.
	var buf bytes.Buffer
	refs := make([]planOpRef, len(plan.Operations))
	for i, op := range plan.Operations {
		encoded, err := json.Marshal(op)
		if err != nil {
			return fmt.Errorf("marshal operation %d: %w", i, err)
		}
		refs[i] = planOpRef{Offset: int64(buf.Len()), Length: len(encoded)}
		buf.Write(encoded)
		buf.WriteByte('\n')
	}

	opsFile := fmt.Sprintf("ops-%s.ndjson", shortHash(plan.ContentSHA256))
	if err := writeFileAtomic(filepath.Join(dir, opsFile), buf.Bytes()); err != nil {
		return fmt.Errorf("write plan operations: %w", err)
	}

	doc := planIndexDoc{
		Format:        plan.Format,
		ConnectionID:  plan.ConnectionID,
		SpecURL:       plan.SpecURL,
		ContentSHA256: plan.ContentSHA256,
		SpecVersion:   plan.SpecVersion,
		Title:         plan.Title,
		BaseURL:       plan.BaseURL,
		EndpointCount: plan.EndpointCount,
		CreatedAt:     plan.CreatedAt,
		Tolerated:     plan.Tolerated,
		Repaired:      plan.Repaired,
		OpsFile:       opsFile,
	}
	for _, res := range plan.Resources {
		entry := planResourceDoc{Name: res.Name, Short: res.Short}
		for _, idx := range res.Operations {
			if idx >= 0 && idx < len(refs) {
				entry.Refs = append(entry.Refs, refs[idx])
			}
		}
		doc.Resources = append(doc.Resources, entry)
	}

	if err := writeJSONAtomic(filepath.Join(dir, planIndexFile), doc); err != nil {
		return fmt.Errorf("write plan index: %w", err)
	}

	// Drop files the new plan does not reference (a previous ops file from an
	// earlier connect of the same API). Failures here are harmless: an orphan
	// file is never read because the index no longer points at it.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			if entry.IsDir() || entry.Name() == planIndexFile || entry.Name() == opsFile {
				continue
			}
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return nil
}

// ReadPlanIndex loads a connection's cached plan index. It returns ErrNoPlan
// when the cache is absent, unreadable, of a different format, or belongs to a
// different source URL — every one of those is a cache miss, not a failure.
func ReadPlanIndex(dataDir string, conn *APIConnection) (*PlanIndex, error) {
	if conn == nil {
		return nil, ErrNoPlan
	}
	data, err := os.ReadFile(filepath.Join(PlanDir(dataDir, conn.ID), planIndexFile))
	if err != nil {
		return nil, ErrNoPlan
	}
	var doc planIndexDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, ErrNoPlan
	}
	if doc.Format != planFormatVersion || doc.ConnectionID != conn.ID || doc.SpecURL != conn.SpecURL {
		return nil, ErrNoPlan
	}
	return &PlanIndex{doc: doc}, nil
}

// ResourceOperations loads the operations of one indexed resource group by
// reading only their byte ranges out of the ops file. It returns ErrNoPlan when
// the resource is not indexed or the store is unreadable, so callers rebuild the
// cache from the source rather than serving a partial command tree.
func (p *PlanIndex) ResourceOperations(dataDir string, conn *APIConnection, resource string) ([]*openapi.Operation, error) {
	if p == nil || conn == nil || p.doc.OpsFile == "" {
		return nil, ErrNoPlan
	}
	var entry *planResourceDoc
	for i := range p.doc.Resources {
		if p.doc.Resources[i].Name == resource {
			entry = &p.doc.Resources[i]
			break
		}
	}
	if entry == nil {
		return nil, ErrNoPlan
	}

	f, err := os.Open(filepath.Join(PlanDir(dataDir, conn.ID), p.doc.OpsFile))
	if err != nil {
		return nil, ErrNoPlan
	}
	defer func() { _ = f.Close() }()

	ops := make([]*openapi.Operation, 0, len(entry.Refs))
	for _, ref := range entry.Refs {
		buf := make([]byte, ref.Length)
		if _, err := f.ReadAt(buf, ref.Offset); err != nil {
			return nil, ErrNoPlan
		}
		var op openapi.Operation
		if err := json.Unmarshal(buf, &op); err != nil {
			return nil, ErrNoPlan
		}
		ops = append(ops, &op)
	}
	return ops, nil
}

// AllOperations loads every operation in the cached plan (the full command tree
// path: `--help`, completion, and any invocation that names no resource).
func (p *PlanIndex) AllOperations(dataDir string, conn *APIConnection) ([]*openapi.Operation, error) {
	if p == nil || conn == nil || p.doc.OpsFile == "" {
		return nil, ErrNoPlan
	}
	data, err := os.ReadFile(filepath.Join(PlanDir(dataDir, conn.ID), p.doc.OpsFile))
	if err != nil {
		return nil, ErrNoPlan
	}

	ops := make([]*openapi.Operation, 0, len(p.doc.Resources))
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var op openapi.Operation
		if err := json.Unmarshal(line, &op); err != nil {
			return nil, ErrNoPlan
		}
		ops = append(ops, &op)
	}
	return ops, nil
}

// ReadPlan loads a connection's entire cached plan (index + every operation).
func ReadPlan(dataDir string, conn *APIConnection) (*SpecPlan, error) {
	idx, err := ReadPlanIndex(dataDir, conn)
	if err != nil {
		return nil, err
	}
	ops, err := idx.AllOperations(dataDir, conn)
	if err != nil {
		return nil, err
	}
	return idx.Plan(ops), nil
}

// RemovePlan deletes a connection's cached plan. A missing plan is not an
// error: disconnecting an API that never cached one is still a disconnect.
func RemovePlan(dataDir, connID string) error {
	if err := os.RemoveAll(PlanDir(dataDir, connID)); err != nil {
		return fmt.Errorf("remove plan: %w", err)
	}
	return nil
}

// shortHash truncates a hex hash for use in file names.
func shortHash(h string) string {
	if len(h) > 8 {
		return h[:8]
	}
	return h
}

// writeFileAtomic writes data to path via a temp file in the same directory,
// then renames it into place.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// writeJSONAtomic writes v as JSON to path atomically.
func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return writeFileAtomic(path, data)
}
