package defaults

// Path constants for issue list, prompt template, log directory, and done file.
// Used by both the main runner (repo defaults) and the TUI (configure).
const (
	IssuesFile     = ".ticket-runner/issues.txt"
	PromptTemplate = ".ticket-runner/prompt.tmpl"
	LogDir         = ".ticket-runs"
	DoneFileName   = ".completed"
)
