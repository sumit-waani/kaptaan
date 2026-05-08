## Execute Phase 2: HTML Templates

### Intent
Create all 4 HTML template files per PRD spec. These are Go `html/template` files with HTMX attributes for dynamic partial swaps and Alpine.js for dark mode + filtering.

### Files to create

1. **`templates/index.html`** — Full page shell:
   - `<body>` with Alpine `x-data="{ dark: false }"` for dark mode
   - Header bar: "TaskBoard" title + dark mode toggle button
   - Create form: `hx-post="/tasks"` targeting `#task-list` with `hx-swap="afterbegin"`
   - Filter buttons: Alpine.js client-side filtering by `data-status`
   - Task list container: `<div id="task-list">` that includes `task_list.html`

2. **`templates/partials/task_list.html`** — Wraps tasks in `<ul>`, iterates with `{{range .Tasks}}`, includes `task_item.html`

3. **`templates/partials/task_item.html`** — Single `<li id="task-{{.ID}}" data-status="{{.Status}}">`:
   - Status badge: clickable, `hx-patch="/tasks/{{.ID}}/status"` for cycling
   - Title with strikethrough class when done
   - Edit button: `hx-get="/tasks/{{.ID}}/edit"`
   - Delete button: `hx-delete="/tasks/{{.ID}}"` with `hx-confirm`

4. **`templates/partials/task_form.html`** — Inline edit form:
   - `hx-put="/tasks/{{.ID}}"` for save
   - Title input, status select, note textarea
   - Cancel button: `hx-get="/tasks/{{.ID}}/view"`

### CDN includes
- HTMX 1.9.x from unpkg
- Alpine.js 3.x from unpkg

### Verification
- Files exist with correct HTMX attributes
- Valid Go template syntax (can be parsed)
- Routes match PRD spec
