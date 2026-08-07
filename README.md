# Keypress

An auto clicker with a visual flowchart interface for creating and executing complex mouse and keyboard automation sequences.

![Keypress Flow Editor](assets/images/KeypressDemo.png)

## Overview

Keypress combines a Go backend for system-level automation with a modern SvelteKit frontend, providing an intuitive drag-and-drop interface for building automation workflows. The application uses Wails v2 to bridge native desktop capabilities with web technologies.

## Technical Stack

**Backend**
- Go 1.25+ with concurrent task execution
- [Wails v3](https://v3.wails.io/) for desktop application framework, system tray and global shortcuts
- [robotgo](https://github.com/go-vgo/robotgo) for cross-platform mouse/keyboard control
- XDG Base Directory specification for file management

**Frontend**
- SvelteKit with TypeScript
- Vite for build tooling and hot reload
- [@xyflow/svelte](https://github.com/xyflow/xyflow) for node-based flow editor
- Tailwind CSS for styling

## Architecture

The application implements a hybrid architecture with clear separation of concerns:

```
┌─────────────────────────────────────────┐
│         Frontend (SvelteKit)            │
│  - Visual Flow Editor                   │
│  - Node-based UI Components             │
│  - Real-time Event Listeners            │
└──────────────┬──────────────────────────┘
               │ Wails Bridge
               │ (Bindings + Events)
┌──────────────┴──────────────────────────┐
│         Backend (Go)                    │
│  - Task Queue System                    │
│  - Automation Execution Engine          │
│  - File Persistence (XDG)               │
│  - System Integration (robotgo)         │
└─────────────────────────────────────────┘
```

### Key Design Patterns

**Backend**
- Worker pool pattern with goroutines for concurrent task execution
- Dependency graph resolution for task ordering
- Event-driven architecture for real-time frontend updates
- Context-based cancellation for graceful shutdown

**Frontend**
- Component-based architecture with Svelte
- Reactive state management with Svelte stores
- Node-based visual programming interface
- Type-safe Wails bindings

## Features

### Visual Flow Editor
- Drag-and-drop node creation and connection
- Real-time flow visualization
- Node types: Start, Mouse Move, Mouse Click, Keyboard Input, Delay
- Visual feedback during execution

### Mouse Automation
- Coordinate-based or relative positioning
- Movement paths: straight line or human-like curves
- Configurable speed with optional randomization
- Click actions (left, right, middle button)
- Multi-click support with configurable delays
- Click-and-drag operations
- Scroll support (vertical and horizontal)

### Keyboard Automation
- Text typing with natural simulation
- Key combinations and shortcuts
- Individual key press actions

### Timing Control
- Fixed delays with millisecond precision
- Random delays within min/max range for human-like behavior

### Execution Engine
- Concurrent task execution with worker pool (3 workers default)
- Automatic dependency resolution based on flow connections
- Real-time status updates via event system
- Graceful error handling and recovery

### File Management
- Auto-save to XDG-compliant data directory
- Automatic loading of last edited flow
- JSON-based flow data format

### Runs While Closed
Keypress is a resident application. Closing the window hides it rather than
quitting, so a macro can run with nothing on screen:

- **System tray** - a "Run macro" submenu lists your saved macros, "Stop macro"
  ends a run in progress, and "Quit Keypress" is the only thing that actually
  exits. Left-clicking the icon brings the window back.
- **Global hotkeys** - the trigger recorded on a macro's **Start node** (the
  Record button in the workspace) is registered as a system-wide shortcut. It
  fires whatever has focus, which is the point: the macro is usually meant to
  act on some other application. The Start node is the only place a hotkey is
  set; the Macros page shows each one read-only, so the two can never disagree.

Hotkeys are claimed at startup and re-synced on every save, so recording one and
saving is all it takes. A combination another application already owns is
skipped with a log line rather than silently stolen, and a recording with no
modifier is refused outright - registered system-wide, a bare `J` would take
that key from every other application on the machine.

## Project Structure

```
Keypress/
├── main.go                 # Wails application entry point (embeds the frontend)
├── Taskfile.yml            # Build entry point (go-task); wraps the wails3 CLI
├── backend/
│   ├── run.go              # Wails app options, window and startup wiring
│   ├── app.go              # Core application logic and Wails bindings
│   ├── tray.go             # System tray icon and menu
│   ├── hotkeys.go          # Global shortcuts bound to saved macros
│   ├── execution.go        # Flowchart execution engine
│   ├── persistence.go      # Saving and loading macros
│   ├── actions_*.go        # Keyboard, mouse, delay and colour actions
│   └── utils/
│       └── fileutils.go    # File system utilities and settings
├── frontend/
│   ├── src/
│   │   ├── routes/         # SvelteKit pages
│   │   ├── lib/
│   │   │   ├── components/ # UI components
│   │   │   ├── stores/     # State management
│   │   │   └── bindings/   # Generated Wails bindings (wails3 generate bindings)
│   │   └── app.html
│   ├── static/             # Static assets
│   └── build/              # Production build output (embedded by main.go)
├── build/                  # Build assets: icons, manifests, platform Taskfiles
└── bin/                    # Compiled desktop application
```

## Development

### Prerequisites
- Go 1.25+ (Wails v3 requires it; Go's toolchain directive will fetch it)
- Node.js 18+
- Wails v3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.4`)
- go-task (`go install github.com/go-task/task/v3/cmd/task@latest`)

### Development Mode
```bash
task dev
```
Runs with hot reload.

### Production Build
```bash
task build
```
Creates `bin/Keypress.exe` with the frontend embedded.

Note that `CGO_ENABLED=1` is set in `Taskfile.yml`. A stock Wails v3 app is pure
Go on Windows, but robotgo drives the real mouse and keyboard through cgo, and
with it disabled the build fails on undefined robotgo symbols.

### Regenerating Bindings
```bash
wails3 generate bindings -d frontend/src/lib/bindings -clean=true -ts -i
```
Runs automatically as part of `task build`. The output lives under `src/lib` so
SvelteKit's `$lib` alias reaches it.

### Frontend Development
```bash
cd frontend
npm install
npm run dev        # Development server
npm run build      # Production build
npm run check      # Type checking
```

## Implementation Details

### Task Queue System

The execution engine uses a buffered channel-based task queue with a configurable worker pool:

```go
type TaskQueue struct {
    tasks   chan Task
    wg      sync.WaitGroup
    ctx     context.Context
    cancel  context.CancelFunc
}
```

Workers process tasks concurrently while respecting dependency constraints defined by flow connections.

### Dependency Resolution

The application builds a dependency graph from flow edges and ensures tasks execute only when all dependencies are met:

```go
dependencies map[string][]string  // source -> targets
completed    map[string]bool      // track completion
```

### Event System

Backend emits events that frontend listens to for real-time updates:
- `task-started`, `task-completed`, `task-error`
- `execution-completed`, `execution-stopped`, `execution-stalled`,
  `execution-nodes-skipped`, `execution-error`
- `save-success`
- `macro-started` - a run begun from the tray or a hotkey rather than from the
  canvas, carrying the macro's id and name so the workspace does not mistake
  those task events for its own graph

The frontend subscribes with `Events.On` from `@wailsio/runtime`; v3 delivers
one event object per listener and puts the payload on its `data`.

### Data Structures

**Node**
```go
type Node struct {
    ID       string
    Type     string
    Data     map[string]interface{}
    Position map[string]float64
}
```

**Edge**
```go
type Edge struct {
    ID     string
    Source string
    Target string
}
```

## Platform Support

- **Windows**: Primary development platform
- **Linux**: Supported via robotgo and Wails
- **macOS**: Supported via robotgo and Wails (requires testing)

## Configuration

- `wails.json` - Wails project configuration
- `go.mod` - Go dependencies
- `frontend/package.json` - Node.js dependencies
- `frontend/svelte.config.js` - Svelte configuration
- `frontend/vite.config.ts` - Vite build configuration
- `frontend/tailwind.config.js` - Tailwind CSS configuration

## License

This project is available for portfolio and demonstration purposes.

## Author

Alexander Marshall
