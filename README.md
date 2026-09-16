# RemGo 🧠⚡

<p align="center">
  <pre align="center">
  ____                 ____       
 |  _ \ ___ _ __ ___  / ___| ___  
 | |_) / _ \ '_ ` _ \| |  _ / _ \ 
 |  _ <  __/ | | | | | |_| | (_) |
 |_| \_\___|_| |_| |_|\____|\___/ 
  </pre>
  <p align="center">
    <strong>An open-source, single-binary, local-first RemNote-like knowledge management outliner & FSRS spaced-repetition engine written in pure Go.</strong>
  </p>
  <p align="center">
    <a href="#features">Features</a> •
    <a href="#quickstart">Quickstart</a> •
    <a href="#remnote-card-syntax">Syntax Guide</a> •
    <a href="#fsrs-engine">FSRS Algorithm</a> •
    <a href="#ai-agent--mcp-server">MCP Server</a> •
    <a href="#rest-api">REST API</a> •
    <a href="#license">License</a>
  </p>
</p>

---

## 💡 What is RemGo?

**RemGo** brings the best of [RemNote](https://remnote.com) and modern spaced-repetition software to a **single, portable, zero-dependency Go binary**.

Built following the ethos of iconic Show HN Go projects like *PocketBase*, *Caddy*, and *Bubbletea*:
- **Single Portable Binary**: Everything—the HTTP server, pure Go SQLite database engine, FSRS scheduler, and modern responsive web UI—is compiled into one standalone binary with `go:embed`.
- **Zero Runtime Dependencies**: No Node.js runtime, no npm packages, no CGO required. Runs on macOS, Linux, and Windows out of the box.
- **Sub-millisecond Performance**: Powered by embedded SQLite in WAL mode with recursive Common Table Expressions (CTEs) for infinite tree structures and FTS5 for instant search.
- **Native AI Agent Integration**: Built-in [Model Context Protocol (MCP)](https://modelcontextprotocol.io) server over `stdio` and HTTP SSE for seamless connection to Claude Desktop, Cursor, and AI agents.

---

## ✨ Features

- **Infinite Outliner**:
  - Zoom into any bullet as an independent document.
  - Keyboard-first UX (`Tab` to indent, `Shift+Tab` to outdent, `Enter` for sibling, `↑`/`↓` to navigate).
  - Collapsible bullet trees with descendant counts.
  - Real-time syntax highlighting for flashcard delimiters, clozes, and references.
- **RemNote-Compatible Flashcard Syntax**:
  - `concept :: definition` (Forward flashcard)
  - `concept ::: definition` (Two-way bidirectional flashcard)
  - `concept ;; descriptor` (Concept/Descriptor card)
  - `prompt ==> target` (List / sequence card)
  - `{{cloze}}` and `{{c1::answer::hint}}` (Fill-in-the-blank cards with hint support)
  - `[[references]]` with bidirectional backlink tracking
- **FSRS Spaced Repetition Engine**:
  - Pure Go implementation of the state-of-the-art **Free Spaced Repetition Scheduler (FSRS-v4.5/v5)**.
  - Predicts memory retrievability and calculates optimal stability intervals.
  - 4-grade rating system: **1 (Again)**, **2 (Hard)**, **3 (Good)**, **4 (Easy)** with live interval previews.
  - **On-Demand Exam Cram Mode**: Practice any deck or subtree without altering scheduled intervals.
- **Interactive 2D Knowledge Graph**:
  - Real-time HTML5 Canvas force-directed physics graph.
  - Visualizes document hierarchies and `[[reference]]` connections with click-to-zoom navigation.
- **Model Context Protocol (MCP) Server**:
  - Built-in JSON-RPC 2.0 stdio server (`remgo mcp`) and HTTP endpoint (`/mcp`).
  - Allows AI assistants to search notes, create hierarchical rems, and review flashcards.
- **Instant Full-Text Search (FTS5)**:
  - Global `Cmd+K` / `Ctrl+K` quick switcher with contextual ancestor breadcrumbs.
- **Data Portability**:
  - One-click Markdown outline export and import.
  - Full JSON export.

---

## 🚀 Quickstart

### Installation

#### Using Go:
```bash
go install github.com/darsheee/remgo/cmd/remgo@latest
```

#### From Source:
```bash
git clone https://github.com/darsheee/remgo.git
cd remgo
make build
./remgo
```

### Running RemGo

Start the outliner and web server (defaults to `http://127.0.0.1:8080`):
```bash
remgo
```

Custom port and data directory:
```bash
remgo -port 9000 -data /path/to/notes
```

Flags:
```text
  -auth
    	Force enable authentication (requires user login)
  -no-auth
    	Disable authentication (single-user mode)
  -api-key string
    	API key / PAT for MCP server authentication
  -token string
    	Bearer/Session token for MCP server authentication
  -user string
    	User ID or username for MCP server in local mode
  -data string
    	Directory to store SQLite database (default "./remgo_data")
  -host string
    	HTTP server host (default "127.0.0.1")
  -port int
    	HTTP server port (or set PORT env var) (default 8080)
  -mcp
    	Start Model Context Protocol (MCP) server over stdio
  -version
    	Print version and exit
```

CLI Commands:
```bash
# Create user account from CLI
remgo create-user -username admin -password secret -role admin

# Create Personal Access Token for MCP agents
remgo create-pat -user admin -name "Cursor MCP"
```

---

## 🔒 Authentication & Multi-Tenancy

RemGo supports production-grade multi-tenancy inspired by tools like **PocketBase** and **Grafana**:

- **Zero-Friction Single-User Mode**: Run with `--no-auth` (or auto-mode before any users exist) for immediate, login-free local note-taking.
- **Multi-Tenant Isolation**: Each user's notes, flashcards, review histories, and references are completely private and scoped by `user_id`.
- **First-User Admin Bootstrap**: When running with `--auth` on a fresh database, RemGo prompts to create the initial administrator account. Any existing starter notes from single-user mode are automatically adopted by the newly created admin.
- **Sessions & JWT**: Secure cryptographic session tokens and HS256 JWTs stored in `HttpOnly` cookies and supported via `Authorization: Bearer <token>`.
- **Personal Access Tokens (PAT)**: Generate `remgo_pat_...` keys in the web UI or CLI for AI agents (Claude Desktop, Cursor, Cline) to authenticate with the MCP server.

---

## 📝 RemNote Card Syntax

RemGo parses your outline notes and automatically generates flashcards:

| Card Type | Syntax Example | Flashcard Generated |
| :--- | :--- | :--- |
| **Forward** | `Golang :: Compiled language by Google` | **Front:** `Golang`<br>**Back:** `Compiled language by Google` |
| **Two-Way** | `Hola ::: Hello` | **Card 1:** `Hola` → `Hello`<br>**Card 2:** `Hello` → `Hola` |
| **Descriptor** | `Mitochondria ;; Powerhouse of the cell` | **Front:** `Mitochondria ;;`<br>**Back:** `Powerhouse of the cell` |
| **List Card** | `Primary colors ==> Red, Green, Blue` | **Front:** `Primary colors ==>`<br>**Back:** `Red, Green, Blue` |
| **Cloze** | `Light speed is {{299,792,458}} m/s` | **Front:** `Light speed is [...] m/s`<br>**Back:** `299,792,458` |
| **Cloze w/ Hint** | `Capital of France is {{Paris::city}}` | **Front:** `Capital of France is [city]`<br>**Back:** `Paris` |
| **References** | `Studying [[Computer Science]]` | Links notes and populates **Linked References** |

---

## 📈 FSRS Engine

RemGo implements the modern **Free Spaced Repetition Scheduler (FSRS)** algorithm in pure Go:

- **Stability ($S$)**: Days required for memory retention to drop to 90%.
- **Difficulty ($D$)**: Innate complexity of the concept (scale 1.0 to 10.0).
- **Retrievability ($R$)**: Probability of recall after elapsed time $t$:
  $$R(t, S) = \left(1 + \frac{19}{81} \cdot \frac{t}{S}\right)^{-0.5}$$
- **Optimal Interval ($I$)**:
  $$I = \text{round}\left(\frac{S}{19/81} \cdot \left(R_{\text{target}}^{-2} - 1\right)\right)$$

### Review Ratings

| Rating | Shortcut | Behavior |
| :---: | :---: | :--- |
| **Again** | `1` | Card forgotten; resets stability, increments lapse count, schedules for immediate relearning. |
| **Hard** | `2` | Recalled with difficulty; slightly grows stability with penalty. |
| **Good** | `3` | Standard recall; updates stability along the optimal memory decay curve. |
| **Easy** | `4` | Effortless recall; stability receives an easy bonus multiplier. |

---

## 🤖 AI Agent & MCP Server

RemGo natively speaks the **Model Context Protocol (MCP)**, allowing local LLMs and AI agents (Claude Desktop, Cursor, Antigravity, OpenDevin, Cline) to interact directly with your second brain.

### Using with Claude Desktop

Add this to your `claude_desktop_config.json`:
```json
{
  "mcpServers": {
    "remgo": {
      "command": "/path/to/remgo",
      "args": ["mcp", "-data", "/path/to/remgo_data"]
    }
  }
}
```

### Using with Cursor (`.cursor/mcp.json`)

```json
{
  "mcpServers": {
    "remgo": {
      "command": "remgo",
      "args": ["mcp"]
    }
  }
}
```

### Exposed MCP Tools

1. `search_rems(query, limit)`: Fast FTS5 full-text search with breadcrumbs.
2. `create_rem(content, parent_id)`: Create bullets and auto-generate flashcards.
3. `get_rem(id)`: Fetch a Rem and its hierarchy.
4. `get_rem_tree(root_id, format)`: Get entire tree as structured JSON or clean Markdown outline.
5. `update_rem(id, content, collapsed)`: Update bullet text or folding.
6. `delete_rem(id)`: Cascade delete bullet and children.
7. `get_due_flashcards(limit)`: Retrieve cards currently due for review.
8. `review_flashcard(card_id, rating, is_cram)`: Submit an FSRS rating (1-4).
9. `get_backlinks(query)`: Find incoming references.
10. `get_card_stats()`: Retrieve knowledge base SRS statistics.

---

## 🌐 REST API

RemGo exposes a clean, zero-latency HTTP JSON API:

```
# Authentication
GET    /api/auth/status        # Check auth mode and login state
POST   /api/auth/setup         # Bootstrap initial admin account
POST   /api/auth/register      # Register new account
POST   /api/auth/login         # Log in (sets cookie & returns token)
POST   /api/auth/logout        # Terminate active session
GET    /api/auth/me            # Get current user profile
GET    /api/auth/keys          # List Personal Access Tokens
POST   /api/auth/keys          # Create Personal Access Token
DELETE /api/auth/keys/{id}     # Revoke Personal Access Token

# Outliner & Documents
GET    /api/tree               # Full outliner tree (optional ?root_id=...)
POST   /api/rems               # Create Rem {"content": "...", "parent_id": "..."}
GET    /api/rems/{id}          # Get Rem details, ancestors, and backlinks
PUT    /api/rems/{id}          # Update Rem {"content": "...", "collapsed": bool}
DELETE /api/rems/{id}          # Cascade delete Rem
POST   /api/rems/{id}/indent   # Indent bullet under preceding sibling
POST   /api/rems/{id}/outdent  # Outdent bullet to parent level
POST   /api/rems/{id}/move     # Reorder sibling bullets

# Search & Graph
GET    /api/search?q=...       # Full-text search (FTS5)
GET    /api/graph              # Knowledge graph nodes & edges

# Spaced Repetition (FSRS)
GET    /api/cards/due          # Fetch due flashcard queue
GET    /api/cards/cram         # Fetch flashcards for cram practice
POST   /api/cards/{id}/review  # Submit review rating {"rating": 1-4, "is_cram": bool}
GET    /api/cards/stats        # Get SRS retention & review metrics

# Backup & Interop
GET    /api/export             # Export as Markdown (?format=json for JSON)
POST   /api/import             # Import Markdown outline

# Model Context Protocol (MCP)
POST   /mcp                    # Model Context Protocol JSON-RPC over HTTP
GET    /mcp/sse                # Model Context Protocol SSE stream
```

---

## ⌨️ Keyboard Shortcuts

| Shortcut | Action |
| :--- | :--- |
| `Tab` | Indent bullet under preceding sibling |
| `Shift + Tab` | Outdent bullet to parent level |
| `Enter` | Create new bullet immediately below |
| `Backspace` (on empty) | Delete bullet and focus previous sibling |
| `↑` / `↓` | Navigate smoothly between bullets |
| `Cmd + K` / `Ctrl + K` | Open global search & quick jump palette |
| `Cmd + 1` | Switch to Notes / Outliner view |
| `Cmd + 2` | Switch to Flashcard Reviewer deck |
| `Cmd + 3` | Switch to 2D Knowledge Graph view |
| `Space` (in Reviewer) | Flip card to reveal answer |
| `1` / `2` / `3` / `4` (in Reviewer) | Rate card Again, Hard, Good, or Easy |

---

## 🏗️ Architecture

```
remgo/
├── cmd/remgo/
│   ├── main.go               # Single binary entrypoint & CLI flags
│   └── main_test.go          # End-to-end integration test
├── internal/
│   ├── srs/                  # Pure Go FSRS algorithm (v4.5/v5)
│   ├── parser/               # RemNote delimiter & cloze parser
│   ├── db/                   # Embedded SQLite (WAL, Recursive CTEs, FTS5)
│   ├── mcp/                  # Model Context Protocol stdio & RPC server
│   ├── api/                  # REST API & HTTP handlers
│   └── web/                  # Embedded HTML5, CSS3, & vanilla JS UI
├── Makefile
├── LICENSE                   # MIT License
└── README.md
```

---

## 📄 License

MIT © [Satya Darshi](https://github.com/darsheee)
