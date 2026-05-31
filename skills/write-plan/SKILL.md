---
name: write-plan
description: Write an implementation plan as a .md file into the project directory. Use when the user says "write a plan", "save the plan", "create a plan file", "document the plan", or wants to persist a plan to disk.
argument-hint: <plan-title> [output-path]
allowed-tools: [Read, Write, Glob, Bash, Edit]
---

# Write Plan

Save a structured implementation plan as a Markdown file in the project directory.

## Arguments

The user invoked this with: $ARGUMENTS

## Behavior

Parse `$ARGUMENTS` as follows:

1. **First token(s)** — the plan title / topic (required). If multi-word, everything before a bare path argument is the title.
2. **Last token** — optional output path override. Detect it as a path if it:
   - starts with `.`, `/`, or a drive letter (`C:\`)
   - contains a path separator (`/` or `\`)
   - ends with `.md`

   If no path override is given, default to `docs/<slugified-title>.md` inside the current working directory.

## Steps

### 1. Resolve output path

- If a path override was provided: use it as-is (create parent directories as needed).
- Otherwise: use `docs/<slug>.md` where `<slug>` is the plan title lowercased with spaces replaced by hyphens and non-alphanumeric characters stripped.
- Check whether the target file already exists. If it does, ask the user whether to overwrite or pick a new name before proceeding.

### 2. Ensure the directory exists

Use a Bash command to create the directory if it does not exist:

```
mkdir -p <parent-dir>
```

### 3. Gather plan content

Draw from the current conversation to populate each section below. If context for a section is missing, use a sensible placeholder or omit the section and note it as "TBD".

### 4. Write the file

Write a `.md` file with the following structure:

```markdown
# <Plan Title>

> **Status:** Draft  
> **Created:** <today's date YYYY-MM-DD>  
> **Author:** <git user name if determinable, else "TBD">

## Overview

<One-paragraph summary of what this plan covers and why.>

## Goals

- <Goal 1>
- <Goal 2>
- ...

## Non-Goals

- <What is explicitly out of scope>

## Approach

<Describe the implementation strategy, architecture decisions, or key choices.>

## Tasks

| # | Task | Owner | Status |
|---|------|-------|--------|
| 1 | <task description> | TBD | [ ] |
| 2 | <task description> | TBD | [ ] |

## Dependencies

- <Any external dependencies, blocked-by items, or prerequisites>

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| <risk> | Low/Med/High | Low/Med/High | <mitigation> |

## Open Questions

- [ ] <Question 1>
- [ ] <Question 2>

## References

- <Links to related docs, PRs, issues, or specs>
```

### 5. Confirm to the user

After writing the file, output one line:

```
Plan written to: <relative-path-from-cwd>
```

Do not output the full plan content again — the user can open the file.

## Edge Cases

- If `$ARGUMENTS` is empty, ask the user for a plan title before proceeding.
- If the resolved path is outside the current working directory tree, warn the user and confirm before writing.
- If writing fails (permissions, etc.), report the error clearly with the full path attempted.