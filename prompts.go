package main

const defaultPromptBody = `You are implementing a fix or feature for GitHub issue #{{ISSUE_NUMBER}}.

## Issue: {{ISSUE_TITLE}}

{{ISSUE_BODY}}

## Instructions

1. Read and understand the issue above thoroughly.
2. Study existing code and related files before making changes.
3. Implement the fix or feature completely. No TODO placeholders.
4. Run the appropriate quality checks and tests for files you modified.
5. Fix any failing tests or lint issues.
6. Create a git commit with either:
   - "fix: <description> (closes #{{ISSUE_NUMBER}})" for bug fixes
   - "feat: <description> (closes #{{ISSUE_NUMBER}})" for features
7. Do not push to remote. Commit locally only.
`

const defaultFilePromptBody = `You are implementing a task described in {{FILE_PATH}}.

## Task: {{ISSUE_TITLE}}

{{ISSUE_BODY}}

## Instructions

1. Read and understand the task above thoroughly.
2. Study existing code and related files before making changes.
3. Implement the fix or feature completely. No TODO placeholders.
4. Run the appropriate quality checks and tests for files you modified.
5. Fix any failing tests or lint issues.
6. Create a git commit with:
   - "fix: <description>" for bug fixes
   - "feat: <description>" for features
7. Do not push to remote. Commit locally only.
`
