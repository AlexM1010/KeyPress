// tasks.go
//
// Dispatch of a queued task to the handler for its node type. The individual
// handlers live in the actions_*.go files.

package main

import (
	"fmt"
	"log"
)

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
		executeStartNodeTask(task, app)

	case "MouseMoveNode":
		executeMouseMoveTask(task, app)

	case "MouseClickNode":
		executeMouseClickTask(task, app)

	// "KeyPressNode" is the key KeyPressNode is registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts, which is
	// what ends up in the saved node's `type`. It replaced the "TypeString" and
	// "KeyTap" cases, which no node ever emitted.
	case "KeyPressNode":
		executeKeyPressTask(task, app)

	case "DelayNode":
		executeDelayTask(task, app)

	// "ColorPickerNode" is the key ColorPickerNode is registered under in
	// frontend/src/lib/components/Workspace/customNodes/nodeTypes.ts, which is
	// what ends up in the saved node's `type`.
	case "ColorPickerNode":
		executeColorPickerTask(task, app)

	default:
		err := fmt.Sprintf("Unknown task type: %s", task.Type)
		log.Printf("%s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err,
		})
	}
}

// executeStartNodeTask marks the beginning of a flow.
func executeStartNodeTask(task Task, app *App) {
	log.Printf("Flow Started - Task ID: %s", task.ID)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "StartNode",
	})
}
