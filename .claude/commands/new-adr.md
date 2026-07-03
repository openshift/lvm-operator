# Create a New ADR

## 1. Determine the Next Number

```bash
ls docs/decisions/[0-9]*.md | sort | tail -1
```

Use the next sequential number (e.g., if last is 0012, use 0013).

## 2. Create the File

Copy the template:
```bash
cp docs/decisions/adr-template.md docs/decisions/NNNN-short-slug.md
```

## 3. Fill the Frontmatter

```yaml
---
status: proposed
date: YYYY-MM-DD
decision-makers: [your name]
consulted: [team members consulted]
informed: [stakeholders informed]
---
```

## 4. Write the ADR

Fill in all sections:
- **Context and Problem Statement**: what decision needs to be made and why
- **Decision Drivers**: constraints and priorities
- **Considered Options**: at least 2 concrete alternatives
- **Decision Outcome**: chosen option with justification
- **Consequences**: good and bad, be honest about tradeoffs

## 5. Update the Index

Add a row to the table in `docs/decisions/index.md`:

```markdown
| [NNNN](NNNN-short-slug.md) | Title | Proposed | YYYY-MM-DD |
```

## 6. Commit

```bash
git add docs/decisions/NNNN-short-slug.md docs/decisions/index.md
git commit -m "docs: add ADR NNNN — short title"
```
