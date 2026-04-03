package main

import "ghir/tui"

const improveCustomMode = "custom"

const defaultImproveCleanupPrompt = `You are performing a continuous cleanup pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Improve readability, style consistency, and small ergonomics.
2. Simplify code where it does not require structural changes.
3. Fix minor duplication, naming, and formatting issues.
4. Prefer light-touch polish over invasive restructuring.

Constraints:
- Preserve existing behavior except where clearly fixing a bug.
- Keep each change reasonably small and reviewable.
- Do not introduce new dependencies unless absolutely necessary.

Expectations:
1. Study the existing code and tests before editing.
2. Apply improvements, updating or adding tests where appropriate.
3. Run relevant tests / checks for the files you touched.
4. Create a git commit with a descriptive message summarizing what you changed.
5. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveQualityPrompt = `You are performing a code-quality debt reduction pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Primary objective:
Deliver one significant code-quality improvement in this run. Do not try to remove many unrelated suppressions or tighten many unrelated rules in a single pass.

Examples of a significant improvement:
- Remove one globally disabled lint rule and fix the resulting violations.
- Replace one broad directory-wide ignore with narrower suppressions and fix the surfaced issues.
- Eliminate one major family of inline suppressions for a specific rule or checker.
- Re-enable one meaningful typecheck or static-analysis rule and make the codebase pass again.

Important:
That one improvement may require many individual code fixes across multiple files to get linting, typechecking, and tests back to green. That is expected.

Workflow:
1. Inspect lint/typecheck configuration, build scripts, and suppression comments.
2. Identify the single highest-value suppression, ignore, or disabled rule to tackle in this run.
3. Remove it or narrow it.
4. Fix all newly surfaced issues needed to restore a clean quality baseline.
5. Re-run the relevant quality checks and ensure the repo is green again.

Delegation:
- When the surfaced fixes naturally split into distinct areas, delegate them to subagents.
- Split work by directory, subsystem, or rule family.
- Give each subagent a clear, non-overlapping ownership area.
- Use subagents for concrete fix batches, not vague exploration.
- Integrate their work and ensure the full repo passes before finishing.

Constraints:
- Do not introduce new blanket ignores.
- Do not replace one broad suppression with another equally broad suppression.
- Do not mix multiple unrelated quality initiatives into one run.
- Preserve behavior unless fixing an obvious bug exposed by the stricter checks.
- Keep the final change reviewable as one coherent improvement.

Expectations:
1. Prefer repo-defined commands such as make q, make lint, make typecheck, or project scripts.
2. Update or add tests when needed.
3. Finish with lint/typecheck/tests clean for the affected checks.
4. Create a git commit with a descriptive message summarizing the quality debt reduction you performed.
5. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveRefactorPrompt = `You are performing a structural refactoring pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Extract functions, classes, or modules to improve structure.
2. Reduce duplication by introducing shared abstractions.
3. Move code between modules for better organization.
4. Improve interfaces and APIs for clarity and maintainability.

Constraints:
- Preserve existing behavior; this is refactoring, not feature work.
- Prefer structural changes over cosmetic ones.
- Keep changes focused and reviewable; avoid rewriting entire files.

Expectations:
1. Study the existing code and tests before editing.
2. Apply structural improvements, updating or adding tests where appropriate.
3. Run relevant tests / checks for the files you touched.
4. Create a git commit with a descriptive message summarizing what you changed.
5. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveSecurityPrompt = `You are performing a continuous security hardening pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Identify and fix obvious security vulnerabilities and misconfigurations.
2. Improve input validation, authentication/authorization checks, and secret handling.
3. Strengthen defaults (e.g., TLS, secure cookies, CSRF, safe file/command usage).

Constraints:
- Prioritize high-impact, low-risk fixes over invasive rewrites.
- Preserve backwards compatibility where possible; call out breaking changes in code comments if needed.

Expectations:
1. Inspect code and configuration for security issues in scope.
2. Implement fixes and, when reasonable, add or update tests.
3. Run relevant tests / checks for the files you touched.
4. Create a git commit with a descriptive message summarizing what you changed.
5. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveBugfixPrompt = `You are performing a focused bug-fix pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Priority order:
1. Reproduce and fix failing bugs first (failing tests, panics, regressions, clearly broken edge cases).
2. If no failing bug is available, find and fix one high-confidence general bug.
3. If no concrete bug stands out, make one small stability hardening fix in a fragile path.

Primary objective:
Deliver one coherent bug fix or one tightly related bug cluster in this run. Keep the change reviewable and centered on a concrete defect.

Constraints:
- Prefer concrete defects over speculative cleanup.
- Avoid broad refactors unless they are required to land the fix safely.
- Preserve existing behavior outside the bug being fixed.

Expectations:
1. Study the relevant code and tests before editing.
2. Reproduce or otherwise confirm the bug when feasible.
3. Implement the fix and add or update regression tests when practical.
4. Run relevant tests / checks for the files you touched.
5. Create a git commit with a descriptive message summarizing what you changed.
6. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveDeadCodePrompt = `You are performing a dead-code removal pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Find functions, types, files, configuration, or assets that are unused.
2. Remove truly dead code and related references safely.
3. Simplify remaining code after removals where obvious.

Constraints:
- Be conservative: if you are not confident something is dead, leave it in place.
- Ensure builds and tests still pass after removals.

Expectations:
1. Use static reasoning and any available tests to confirm code is unused before deleting.
2. Remove dead code and update call sites, imports, and configuration accordingly.
3. Run relevant tests / checks for the files you touched.
4. Create a git commit with a descriptive message summarizing what you changed.
5. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveDocsPrompt = `You are performing a documentation pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Add or improve README, docstrings, API docs, and inline comments.
2. Document public APIs and non-obvious logic.
3. Keep docs accurate and in sync with the code.

Constraints:
- Do not invent or assume behavior; document what the code actually does.
- Prefer clarity over brevity.

Expectations:
1. Study the code before writing or editing docs.
2. Add or update documentation where it adds value.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveTestsPrompt = `You are performing a test coverage pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Find untested or under-tested code.
2. Add unit tests, edge cases, and integration tests where they add value.
3. Improve test quality (clarity, maintainability, meaningful assertions).

Constraints:
- Do not change production code except to make it testable.
- Prefer focused tests over broad, brittle ones.

Expectations:
1. Identify gaps in test coverage and add tests where valuable.
2. Run the test suite and fix any failures.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveDepsPrompt = `You are performing a dependency and tech-debt pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Update outdated dependencies to compatible newer versions.
2. Remove unused dependencies.
3. Fix deprecation warnings and migrate to supported APIs.

Constraints:
- Be conservative: prefer minor/patch updates over major upgrades unless clearly safe.
- Ensure the project still builds and tests pass after changes.

Expectations:
1. Inspect dependency files and lockfiles.
2. Apply updates and fix any breakage.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImprovePerfPrompt = `You are performing a performance improvement pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Find obvious inefficiencies (N+1 queries, redundant work, unnecessary allocations).
2. Apply low-risk, high-impact optimizations.
3. Improve algorithmic complexity where clearly beneficial.

Constraints:
- Preserve correctness; do not optimize at the cost of bugs.
- Prefer simple optimizations over micro-optimizations.

Expectations:
1. Profile or reason about hotspots before changing code.
2. Apply improvements and verify behavior is unchanged.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveA11yPrompt = `You are performing an accessibility improvement pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Improve accessibility in web UIs: ARIA attributes, keyboard navigation, focus management.
2. Fix contrast issues and ensure content is perceivable.
3. Make interactive elements properly labeled and operable.

Constraints:
- Preserve existing behavior and styling where possible.
- Follow WCAG guidelines where applicable.

Expectations:
1. Inspect UI components and markup for accessibility issues.
2. Apply fixes and verify they work with screen readers or keyboard-only navigation if feasible.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveErrorsPrompt = `You are performing an error-handling improvement pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Improve error messages to be more descriptive and actionable.
2. Add or fix logging around failure paths.
3. Avoid swallowing errors; propagate or handle them explicitly.

Constraints:
- Preserve existing error-handling semantics where they are intentional.
- Do not introduce new dependencies solely for logging.

Expectations:
1. Find places where errors are ignored, poorly logged, or have unhelpful messages.
2. Apply improvements while keeping behavior correct.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveTypesPrompt = `You are performing a type-safety and static-analysis pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Add or tighten types where they improve correctness and IDE support.
2. Fix type errors and satisfy stricter linters.
3. Replace dynamic patterns with typed equivalents where practical.

Constraints:
- Do not break builds or tests.
- Prefer incremental improvements over large rewrites.

Expectations:
1. Run type checkers and linters to find issues.
2. Fix type errors and add types where valuable.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

const defaultImproveLoggingPrompt = `You are performing a logging and observability pass on this git repository.

Optional focus path: {{SCOPE}} (if empty, consider the whole repo)

Goals:
1. Add structured logging where it helps debugging and operations.
2. Improve log levels, context, and consistency.
3. Add metrics or tracing where the project already supports them.

Constraints:
- Do not log sensitive data (secrets, PII).
- Avoid excessive logging that would harm performance or noise.

Expectations:
1. Identify key operations and failure paths that would benefit from better observability.
2. Add or improve logging and related instrumentation.
3. Create a git commit with a descriptive message summarizing what you changed.
4. Do not push or open PRs yourself; the surrounding tool will handle that.
`

// improveModes is the ordered list of modes for mixed-mode cycling (excludes "mixed").
// Derived from tui.ValidImproveModes so prompt keys stay in sync with CLI validation.
var improveModes = append([]string(nil), tui.ValidImproveModes[1:]...)

var improveTemplateByMode = map[string]struct {
	fileName string
	builtIn  string
}{
	"cleanup":   {"improve-cleanup.tmpl", defaultImproveCleanupPrompt},
	"quality":   {"improve-quality.tmpl", defaultImproveQualityPrompt},
	"refactor":  {"improve-refactor.tmpl", defaultImproveRefactorPrompt},
	"security":  {"improve-security.tmpl", defaultImproveSecurityPrompt},
	"bugfix":    {"improve-bugfix.tmpl", defaultImproveBugfixPrompt},
	"dead-code": {"improve-dead-code.tmpl", defaultImproveDeadCodePrompt},
	"docs":      {"improve-docs.tmpl", defaultImproveDocsPrompt},
	"tests":     {"improve-tests.tmpl", defaultImproveTestsPrompt},
	"deps":      {"improve-deps.tmpl", defaultImproveDepsPrompt},
	"perf":      {"improve-perf.tmpl", defaultImprovePerfPrompt},
	"a11y":      {"improve-a11y.tmpl", defaultImproveA11yPrompt},
	"errors":    {"improve-errors.tmpl", defaultImproveErrorsPrompt},
	"types":     {"improve-types.tmpl", defaultImproveTypesPrompt},
	"logging":   {"improve-logging.tmpl", defaultImproveLoggingPrompt},
}

var improveModeLabels = map[string]string{
	"cleanup":   "cleanup",
	"custom":    "custom prompt",
	"mixed":     "mixed-mode",
	"multi":     "multi-mode",
	"quality":   "code quality",
	"refactor":  "refactor",
	"security":  "security hardening",
	"bugfix":    "bug fixes",
	"dead-code": "remove dead code",
	"docs":      "documentation",
	"tests":     "test coverage",
	"deps":      "dependencies",
	"perf":      "performance",
	"a11y":      "accessibility",
	"errors":    "error handling",
	"types":     "type safety",
	"logging":   "logging",
}
