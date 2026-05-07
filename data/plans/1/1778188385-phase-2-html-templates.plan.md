## Phase 2: HTML Templates

### Intent
Create all 4 HTML template files as specified in the PRD. These are the server-rendered views that HTMX will swap dynamically.

### Files to create

1. **`templates/index.html`** — Full page shell:
   - Header with "TaskBoard" title + dark mode toggle (Alpine.js)
   - Create form (hx-post to `/tasks`)
   - Filter buttons (Alpine.js client-side filtering)
   - Task list container (`#task-list`)

2. **`templates/partials/task_list.html`** — `<ul>` of tasks, iterates over tasks slice, includes each `task_item.html`

3. **`templates/partials/task_item.html`** — Single `<li>` row:
   - Status badge (clickable, hx-patch to cycle)
   - Title (strikethrough when done)
   - Edit button (hx-get to `/tasks/{id}/edit`)
   - Delete button (hx-delete with confirm)

4. **`templates/partials/task_form.html`** — Inline edit form:
   - Title input, status select, note textarea
   - Save button (hx-put)
   - Cancel button (hx-get to restore view)

### CDN includes
- HTMX 1.9.x from unpkg
- Alpine.js 3.x from unpkg

### Verification
- Files exist with correct structure
- Template syntax is valid (can be parsed by `html/template`)
- HTMX attributes match the routes from PRD
