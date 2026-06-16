// app.go

package main

import (
	"Keypress/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/go-vgo/robotgo"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Update the App struct
type App struct {
	ctx          context.Context
	taskQueue    *TaskQueue
	isExecuting  bool
	execMutex    sync.Mutex
	completed    map[string]bool
	completedMux sync.Mutex
	dependencies map[string][]string
	nodeMap      map[string]Node
	notifyCh     chan string
}

// Node represents a single node in the flowchart.
type Node struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Data     map[string]interface{} `json:"data"`
	Position map[string]float64     `json:"position"`
}

// Edge represents a connection between two nodes.
type Edge struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceHandle string `json:"sourceHandle,omitempty"`
	TargetHandle string `json:"targetHandle,omitempty"`
	Type         string `json:"type,omitempty"`
}

// FlowData represents the complete flow chart data structure
type FlowData struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Task represents a single executable task.
type Task struct {
	ID   string
	Type string
	Data map[string]interface{}
}

// TaskQueue manages the queue of tasks to be executed.
type TaskQueue struct {
	tasks   chan Task
	wg      sync.WaitGroup
	ctx     context.Context
	cancel  context.CancelFunc
	started bool
	mutex   sync.Mutex
	app     *App
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		taskQueue:    NewTaskQueue(nil, 100),
		completed:    make(map[string]bool),
		notifyCh:     make(chan string, 100),
		dependencies: make(map[string][]string),
	}
}

// Initialize auth state on startup
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.taskQueue.app = a  // Set the app reference in TaskQueue
	a.taskQueue.Start(3) // Start 3 workers by default
}

// SaveFile shows the native Save File dialog and saves the flow data to the selected location.
func (a *App) SaveFile(flowData FlowData) (string, error) {
	// First attempt to save to XDG data directory
	defaultPath, err := utils.SaveFlowData(flowData, "default_flow")
	if err != nil {
		log.Printf("Failed to save to default location: %v", err)
		// Continue to allow manual save
		return "", err
	} else {
		// Emit save success event
		a.emitEvent("save-success", fmt.Sprintf("Flow saved to %s", defaultPath))
		return defaultPath, nil
	}
}

// LoadLastFile attempts to load the last opened file's data
func (a *App) LoadLastFile() (*FlowData, error) {
	// Get the last opened file path
	lastFilePath, err := utils.GetLastOpenedFile()
	if err != nil {
		return nil, fmt.Errorf("failed to get last opened file: %w", err)
	}

	// If no last file exists, return nil without error
	if lastFilePath == "" {
		return nil, nil
	}

	// Read the file
	data, err := os.ReadFile(lastFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read last file: %w", err)
	}

	// Parse the JSON data
	var flowData FlowData
	if err := json.Unmarshal(data, &flowData); err != nil {
		return nil, fmt.Errorf("failed to parse flow data: %w", err)
	}

	return &flowData, nil
}

//=============================================== Flow Execution ===============================================

// NewTaskQueue initializes a new TaskQueue.
func NewTaskQueue(app *App, bufferSize int) *TaskQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskQueue{
		tasks:  make(chan Task, bufferSize),
		ctx:    ctx,
		cancel: cancel,
		app:    app,
	}
}

// Start initializes worker goroutines to process tasks.
func (q *TaskQueue) Start(workerCount int) {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if q.started {
		return
	}
	q.started = true
	for i := 0; i < workerCount; i++ {
		q.wg.Add(1)
		go q.worker(i)
	}
	log.Printf("TaskQueue started with %d workers", workerCount)
}

// worker processes tasks from the queue.
func (q *TaskQueue) worker(workerID int) {
	defer q.wg.Done()
	log.Printf("Worker %d started", workerID)
	for {
		select {
		case task, ok := <-q.tasks:
			if !ok {
				log.Printf("Worker %d stopping: task channel closed", workerID)
				return
			}
			log.Printf("Worker %d processing task %s of type %s", workerID, task.ID, task.Type)
			q.app.emitEvent("task-started", task.ID)
			executeTask(task, q.app)
			q.app.emitEvent("task-completed", task.ID)
			// Notify task completion for dependency handling
			q.app.notifyTaskCompletion(task.ID)
		case <-q.ctx.Done():
			log.Printf("Worker %d stopping: context canceled", workerID)
			return
		}
	}
}

// Enqueue adds a task to the queue.
func (q *TaskQueue) Enqueue(task Task) {
	select {
	case q.tasks <- task:
		log.Printf("Enqueued task %s of type %s", task.ID, task.Type)
	case <-q.ctx.Done():
		log.Println("Task queue is closed. Cannot enqueue task:", task.ID)
	}
}

// Stop gracefully shuts down the task queue.
func (q *TaskQueue) Stop() {
	q.mutex.Lock()
	defer q.mutex.Unlock()
	if !q.started {
		return
	}
	q.cancel()
	close(q.tasks)
	q.wg.Wait()
	q.started = false
	log.Println("TaskQueue has been stopped")
}

// executeTask performs the action based on the task type.
func executeTask(task Task, app *App) {
	log.Printf("Starting execution of task ID: %s, Type: %s", task.ID, task.Type)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("Recovered from panic in task %s: %v", task.ID, r)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  fmt.Sprintf("panic: %v", r),
			})
		}
		log.Printf("Completed execution of task ID: %s", task.ID)
	}()

	switch task.Type {
	case "StartNode":
		log.Printf("Flow Started - Task ID: %s", task.ID)
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "StartNode",
		})

	case "MouseMoveNode":
		log.Printf("MoveMouse task starting - Data: %+v", task.Data)

		// Extract and validate position configurations
		startPos, ok1 := task.Data["startPosition"].(map[string]interface{})
		endPos, ok2 := task.Data["endPosition"].(map[string]interface{})
		if !ok1 || !ok2 {
			err := "Invalid or missing position configurations"
			log.Printf("MoveMouse error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		// Get current mouse position for 'Mouse' type positions
		currentX, currentY := robotgo.Location()

		// Resolve start position
		var startX, startY float64
		if startPos["type"] == "Mouse" {
			startX, startY = float64(currentX), float64(currentY)
		} else {
			coords, ok := startPos["coordinates"].(map[string]interface{})
			if !ok {
				err := "Invalid start coordinates"
				log.Printf("MoveMouse error: %s for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  err,
				})
				return
			}
			startX = coords["x"].(float64)
			startY = coords["y"].(float64)
		}

		// Move to start position if not already there
		robotgo.Move(int(startX), int(startY))

		// Resolve end position
		var endX, endY float64
		if endPos["type"] == "Mouse" {
			endX, endY = float64(currentX), float64(currentY)
		} else {
			coords, ok := endPos["coordinates"].(map[string]interface{})
			if !ok {
				err := "Invalid end coordinates"
				log.Printf("MoveMouse error: %s for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  err,
				})
				return
			}
			endX = coords["x"].(float64)
			endY = coords["y"].(float64)
		}

		// Extract movement settings
		speed := task.Data["speed"].(map[string]interface{})
		speedType := speed["type"].(string)
		speedValue := speed["value"].(float64)
		randomize := speed["randomize"].(bool)
		variance := speed["variance"].(float64)
		pathType := task.Data["pathType"].(string)
		dragWhileMoving := task.Data["dragWhileMoving"].(bool)

		// Calculate final speed with randomization if enabled
		finalSpeed := speedValue
		if randomize {
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			varianceAmount := speedValue * (variance / 100.0)
			finalSpeed += (r.Float64()*2 - 1) * varianceAmount
		}

		// Set mouse movement speed based on configuration
		robotgo.MouseSleep = int(finalSpeed) // Convert to appropriate sleep value

		// Start drag if required
		if dragWhileMoving {
			if err := robotgo.MouseDown("left"); err != nil {
				log.Printf("MouseDown error: %v for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  fmt.Sprintf("MouseDown failed: %v", err),
				})
				return
			}
		}

		// Execute movement based on configuration
		if speedType == "Instant" {
			robotgo.Move(int(endX), int(endY))
		} else {
			mouseDelay := int(finalSpeed)
			if pathType == "Straight" {
				// Use MoveSmooth with minimal randomization for straight path TODO: use relative?
				success := robotgo.MoveSmooth(int(endX), int(endY), 1.0, 1.2, mouseDelay)
				if !success {
					log.Printf("MoveSmooth failed for task %s", task.ID)
					// Fallback to regular move
					robotgo.Move(int(endX), int(endY))
				}
			} else {
				// Use MoveSmooth with more randomization for human-like movement
				success := robotgo.MoveSmooth(int(endX), int(endY), 1.0, 2.0, mouseDelay)
				if !success {
					log.Printf("MoveSmooth failed for task %s", task.ID)
					// Fallback to regular move
					robotgo.Move(int(endX), int(endY))
				}
			}
		}

		// Release drag if active
		if dragWhileMoving {
			if err := robotgo.MouseUp("left"); err != nil {
				log.Printf("MouseUp error: %v for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  fmt.Sprintf("MouseUp failed: %v", err),
				})
				return
			}
		}

		// Reset mouse sleep to default
		robotgo.MouseSleep = 100

		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "MoveMouse",
		})

	case "MouseClickNode":
		log.Printf("Click task starting - Data: %+v", task.Data)

		// Get buttonType
		buttonType, ok := task.Data["buttonType"].(string)
		if !ok {
			err := fmt.Sprintf("Invalid buttonType value: %v", task.Data["buttonType"])
			log.Printf("Click error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		// Map buttonType to robotgo button
		if buttonType != "left" && buttonType != "right" && buttonType != "middle" {
			err := fmt.Sprintf("Unsupported buttonType: %s", buttonType)
			log.Printf("Click error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		// Get numberOfClicks
		numberOfClicks, ok := task.Data["numberOfClicks"].(float64)
		if !ok {
			err := fmt.Sprintf("Invalid numberOfClicks value: %v", task.Data["numberOfClicks"])
			log.Printf("Click error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		// Get clickDelay
		clickDelay, ok := task.Data["clickDelay"].(float64)
		if !ok {
			clickDelay = 0.1 // Default delay of 100ms
		}
		clickDuration := time.Duration(clickDelay) * time.Millisecond

		// Get pressReleaseDelay and releaseAfterPress
		pressReleaseDelay, ok := task.Data["pressReleaseDelay"].(float64)
		if !ok {
			pressReleaseDelay = 0.1 // Default press duration of 100ms
		}
		pressDuration := time.Duration(pressReleaseDelay) * time.Millisecond

		releaseAfterPress, _ := task.Data["releaseAfterPress"].(bool)

		// Perform the click actions
		if numberOfClicks > 0 {
			log.Printf("Performing %v clicks with %v delay and %v press duration",
				numberOfClicks, clickDuration, pressDuration)

			for i := 0; i < int(numberOfClicks); i++ {
				if releaseAfterPress {
					robotgo.MouseDown(buttonType)
					time.Sleep(pressDuration)
					robotgo.MouseUp(buttonType)
				} else {
					robotgo.Click(buttonType)
				}

				if i < int(numberOfClicks)-1 {
					time.Sleep(clickDuration)
				}
			}
		}

		// Get scroll options
		scrollDirections, _ := task.Data["scrollDirection"].([]interface{})
		scrollLines, hasScrollLines := task.Data["scrollLines"].(float64)

		// Handle scrolling if configured
		if len(scrollDirections) > 0 && hasScrollLines && scrollLines > 0 {
			for _, dir := range scrollDirections {
				direction, ok := dir.(string)
				if !ok {
					continue
				}

				scrollAmount := int(scrollLines)
				log.Printf("Scrolling %s with %v lines", direction, scrollAmount)

				switch direction {
				case "Vertical":
					// For vertical scrolling, positive is down, negative is up
					robotgo.ScrollDir(scrollAmount, "down")
				case "Horizontal":
					// For horizontal scrolling, we use the x,y coordinates method
					// Positive scrollAmount moves right, negative moves left
					robotgo.Scroll(scrollAmount, 0)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}

		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "Click",
		})

	case "TypeString":
		log.Printf("TypeString task starting - Data: %+v", task.Data)
		text, ok := task.Data["text"].(string)
		if !ok {
			err := fmt.Sprintf("Invalid text value: %v", task.Data["text"])
			log.Printf("TypeString error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		log.Printf("Typing text: %s", text)
		robotgo.TypeStr(text)
		time.Sleep(100 * time.Millisecond)
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "TypeString",
		})

	case "KeyTap":
		log.Printf("KeyTap task starting - Data: %+v", task.Data)
		key, ok := task.Data["key"].(string)
		if !ok {
			err := fmt.Sprintf("Invalid key value: %v", task.Data["key"])
			log.Printf("KeyTap error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}
		log.Printf("Tapping key: %s", key)
		robotgo.KeyTap(key)
		time.Sleep(100 * time.Millisecond)
		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "KeyTap",
		})
	case "DelayNode":
		log.Printf("Delay task starting - Data: %+v", task.Data)

		// Get delayType
		delayType, ok := task.Data["delayType"].(string)
		if !ok {
			err := fmt.Sprintf("Invalid delayType value: %v", task.Data["delayType"])
			log.Printf("Delay error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		switch delayType {
		case "Fixed":
			// Get time
			timeFloat, ok := task.Data["time"].(float64)
			if !ok {
				err := fmt.Sprintf("Invalid time value: %v", task.Data["time"])
				log.Printf("Delay error: %s for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  err,
				})
				return
			}
			duration := time.Duration(timeFloat) * time.Millisecond
			log.Printf("Executing fixed delay of %v milliseconds", timeFloat)
			time.Sleep(duration)

		case "Random":
			// Get minTime and maxTime
			minTimeFloat, ok1 := task.Data["minTime"].(float64)
			maxTimeFloat, ok2 := task.Data["maxTime"].(float64)
			if !ok1 || !ok2 {
				err := fmt.Sprintf("Invalid minTime or maxTime values: minTime=%v, maxTime=%v", task.Data["minTime"], task.Data["maxTime"])
				log.Printf("Delay error: %s for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  err,
				})
				return
			}
			if minTimeFloat > maxTimeFloat {
				err := "minTime cannot be greater than maxTime"
				log.Printf("Delay error: %s for task %s", err, task.ID)
				app.emitEvent("task-error", map[string]interface{}{
					"taskID": task.ID,
					"error":  err,
				})
				return
			}

			// Generate random delay using a local random generator
			r := rand.New(rand.NewSource(time.Now().UnixNano()))
			delay := minTimeFloat + r.Float64()*(maxTimeFloat-minTimeFloat)
			duration := time.Duration(delay) * time.Millisecond
			log.Printf("Executing random delay between %v and %v milliseconds. Selected delay: %v milliseconds", minTimeFloat, maxTimeFloat, delay)
			time.Sleep(duration)

		default:
			err := fmt.Sprintf("Unsupported delayType: %s", delayType)
			log.Printf("Delay error: %s for task %s", err, task.ID)
			app.emitEvent("task-error", map[string]interface{}{
				"taskID": task.ID,
				"error":  err,
			})
			return
		}

		app.emitEvent("task-success", map[string]interface{}{
			"taskID": task.ID,
			"type":   "Delay",
		})

	default:
		err := fmt.Sprintf("Unknown task type: %s", task.Type)
		log.Printf("%s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
	}
}

// StartExecution receives the flowchart data and starts execution.
func (a *App) StartExecution(flow string) error {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if a.isExecuting {
		log.Println("StartExecution called but execution is already in progress")
		return errors.New("execution already in progress")
	}
	a.isExecuting = true
	log.Println("Execution started")
	defer func() {
		if r := recover(); r != nil {
			a.setExecuting(false)
			log.Printf("Recovered from panic in StartExecution: %v", r)
			a.emitEvent("execution-error", fmt.Sprintf("panic: %v", r))
		}
	}()

	var flowData FlowData
	err := json.Unmarshal([]byte(flow), &flowData)
	if err != nil {
		a.isExecuting = false
		log.Printf("Failed to unmarshal flowchart: %v", err)
		return fmt.Errorf("invalid flowchart data: %w", err)
	}

	// Validate flowchart
	if len(flowData.Nodes) == 0 {
		a.isExecuting = false
		log.Println("Flowchart validation failed: no nodes found")
		return errors.New("flowchart must contain at least one node")
	}

	// Build node map for easy access
	a.nodeMap = make(map[string]Node)
	for _, node := range flowData.Nodes {
		a.nodeMap[node.ID] = node
	}

	// Filter out unconnected nodes
	connectedNodeIDs := make(map[string]bool)
	for _, edge := range flowData.Edges {
		connectedNodeIDs[edge.Source] = true
		connectedNodeIDs[edge.Target] = true
	}

	connectedNodeMap := make(map[string]Node)
	for id := range connectedNodeIDs {
		if node, exists := a.nodeMap[id]; exists {
			connectedNodeMap[id] = node
		}
	}
	a.nodeMap = connectedNodeMap

	// Build dependencies
	a.dependencies = make(map[string][]string)
	for _, edge := range flowData.Edges {
		a.dependencies[edge.Source] = append(a.dependencies[edge.Source], edge.Target)
	}

	// Initialize completed map
	a.completedMux.Lock()
	a.completed = make(map[string]bool)
	a.completedMux.Unlock()

	// Enqueue initial tasks (StartNode)
	startNode, err := a.findStartNode(flowData.Nodes)
	if err != nil {
		a.isExecuting = false
		log.Printf("StartExecution failed: %v", err)
		return err
	}

	task := Task{
		ID:   startNode.ID,
		Type: startNode.Type,
		Data: startNode.Data,
	}
	a.taskQueue.Enqueue(task)

	// Start a goroutine to handle task completions
	go a.handleCompletions()

	return nil
}

// findStartNode locates the StartNode in the flowchart.
func (a *App) findStartNode(nodes []Node) (Node, error) {
	for _, node := range nodes {
		if node.Type == "StartNode" {
			return node, nil
		}
	}
	return Node{}, errors.New("no Start node found in flowchart")
}

// notifyTaskCompletion is called by TaskQueue workers when a task is completed.
func (a *App) notifyTaskCompletion(taskID string) {
	select {
	case a.notifyCh <- taskID:
		log.Printf("Task %s completion notified", taskID)
	default:
		log.Printf("Notification channel full. Unable to notify completion of task %s", taskID)
	}
}

// handleCompletions listens for completed tasks and enqueues dependent tasks.
func (a *App) handleCompletions() {
	for {
		select {
		case taskID := <-a.notifyCh:
			a.completedMux.Lock()
			a.completed[taskID] = true
			a.completedMux.Unlock()
			log.Printf("Task %s marked as completed", taskID)

			// Find dependent tasks
			dependents := a.dependencies[taskID]
			for _, depID := range dependents {
				if a.canEnqueue(depID) {
					node, exists := a.nodeMap[depID]
					if !exists {
						log.Printf("Node ID %s not found in nodeMap", depID)
						continue
					}
					task := Task{
						ID:   node.ID,
						Type: node.Type,
						Data: node.Data,
					}
					a.taskQueue.Enqueue(task)
				}
			}

			// Check if all tasks are completed
			a.completedMux.Lock()
			allCompleted := len(a.completed) == len(a.nodeMap)
			a.completedMux.Unlock()

			if allCompleted {
				a.setExecuting(false)
				a.emitEvent("execution-completed", nil)
				log.Println("All tasks completed. Execution finished.")
				return
			}
		case <-a.taskQueue.ctx.Done():
			a.setExecuting(false)
			log.Println("Execution stopped due to task queue cancellation")
			a.emitEvent("execution-stopped", nil)
			return
		case <-time.After(10 * time.Minute):
			// Optional: Timeout to prevent indefinite waiting
			a.setExecuting(false)
			a.taskQueue.Stop()
			log.Println("Execution timed out after 10 minutes")
			a.emitEvent("execution-timed-out", nil)
			return
		}
	}
}

// canEnqueue checks if all dependencies of a node are met.
func (a *App) canEnqueue(nodeID string) bool {
	var deps []string
	for source, targets := range a.dependencies {
		for _, target := range targets {
			if target == nodeID {
				deps = append(deps, source)
			}
		}
	}

	a.completedMux.Lock()
	defer a.completedMux.Unlock()
	for _, dep := range deps {
		if !a.completed[dep] {
			return false
		}
	}
	return true
}

// StopExecution stops the ongoing execution.
func (a *App) StopExecution() {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()

	if !a.isExecuting {
		log.Println("StopExecution called but no execution is in progress")
		return
	}

	a.taskQueue.Stop()
	a.setExecuting(false)
	a.emitEvent("execution-stopped", nil)
	log.Println("Execution has been stopped by the user")
}

// setExecuting updates the execution state.
func (a *App) setExecuting(state bool) {
	a.execMutex.Lock()
	a.isExecuting = state
	a.execMutex.Unlock()
	log.Printf("Execution state set to: %v", state)
}

// emitEvent emits an event to the frontend.
func (a *App) emitEvent(event string, payload interface{}) {
	runtime.EventsEmit(a.ctx, event, payload)
}

// GetIsExecuting returns the current execution state.
func (a *App) GetIsExecuting() bool {
	a.execMutex.Lock()
	defer a.execMutex.Unlock()
	return a.isExecuting
}
