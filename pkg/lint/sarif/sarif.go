package sarif

// Report represents a SARIF 2.1.0 report containing one or more runs.
type Report struct {
	Version string `json:"version"`
	Schema  string `json:"$schema"`
	Runs    []Run  `json:"runs"`
}

// NewReport creates a new SARIF report with the given runs.
func NewReport(runs ...Run) Report {
	return Report{
		Version: "2.1.0",
		Schema:  "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json",
		Runs:    runs,
	}
}

// Run represents a single run of a static analysis tool.
type Run struct {
	Tool      Tool       `json:"tool"`
	Artifacts []Artifact `json:"artifacts,omitempty"`
	Results   []Result   `json:"results"`
}

// NewRun creates a new run with the given tool name and information URI.
func NewRun(name, infoURI string) Run {
	return Run{
		Tool: Tool{
			Driver: Driver{
				Name:    name,
				InfoURI: infoURI,
				Rules:   []Rule{},
			},
		},
		Results: []Result{},
	}
}

// WithRules adds rules to the run and returns the modified run.
func (r Run) WithRules(rules ...Rule) Run {
	r.Tool.Driver.Rules = append(r.Tool.Driver.Rules, rules...)

	return r
}

// WithArtifacts adds artifacts to the run and returns the modified run.
func (r Run) WithArtifacts(artifacts ...Artifact) Run {
	r.Artifacts = append(r.Artifacts, artifacts...)

	return r
}

// WithResults adds results to the run and returns the modified run.
func (r Run) WithResults(results ...Result) Run {
	r.Results = append(r.Results, results...)

	return r
}

type Tool struct {
	Driver Driver `json:"driver"`
}

type Driver struct {
	Name    string `json:"name"`
	InfoURI string `json:"informationUri,omitempty"`
	Rules   []Rule `json:"rules"`
}

// Rule represents a static analysis rule definition.
type Rule struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	ShortDescription Message  `json:"shortDescription"`
	FullDescription  *Message `json:"fullDescription,omitempty"`
}

// NewRule creates a new rule with the given ID, name, and a short description.
func NewRule(id, name, shortDescription string) Rule {
	return Rule{
		ID:               id,
		Name:             name,
		ShortDescription: Message{Text: shortDescription},
	}
}

// Artifact represents a file or other artifact analyzed by the tool.
type Artifact struct {
	Location RunArtifactLocation `json:"location"`
}

// NewArtifact creates a new artifact with the given URI.
func NewArtifact(uri string) Artifact {
	return Artifact{RunArtifactLocation{
		URI: uri,
	}}
}

type RunArtifactLocation struct {
	URI string `json:"uri"`
}

// Result represents a single finding or result from the analysis.
type Result struct {
	Level     string     `json:"level"`
	Message   Message    `json:"message"`
	Locations []Location `json:"locations"`
	RuleID    string     `json:"ruleId"`
	RuleIndex int        `json:"ruleIndex"`
}

// NewResult creates a new result with the given level, message, rule ID, rule index, and locations.
func NewResult(level, message, ruleID string, ruleIndex int, locs ...Location) Result {
	return Result{
		Level:     level,
		Message:   Message{Text: message},
		RuleID:    ruleID,
		RuleIndex: ruleIndex,
		Locations: locs,
	}
}

type Message struct {
	Text     string `json:"text"`
	Markdown string `json:"markdown,omitempty"`
}

// Location represents the location of a result in a source file.
type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

// NewLocation creates a new location with the given URI, artifact index, start line, and start column.
func NewLocation(uri string, artifactIndex, startLine, startCol int) Location {
	return Location{PhysicalLocation{
		ArtifactLocation: ResultArtifactLocation{
			URI:   uri,
			Index: artifactIndex,
		},
		Region: Region{
			StartLine:   startLine,
			StartColumn: startCol,
		},
	}}
}

type PhysicalLocation struct {
	ArtifactLocation ResultArtifactLocation `json:"artifactLocation"`
	Region           Region                 `json:"region"`
}

type ResultArtifactLocation struct {
	URI   string `json:"uri"`
	Index int    `json:"index"`
}

type Region struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
}
