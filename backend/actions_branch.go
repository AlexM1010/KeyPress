// actions_branch.go
//
// The two halves of run state, and the node between them.
//
// A value gets into a run's scratchpad through the optional "store result as"
// field on a node that already produces a result - the colour Wait For Color
// matched, the position Mouse Move ended at - and comes back out through a
// Branch node's condition. Both halves are here because they are one feature:
// the spelling of a name, and what a comparison of two stored values means, are
// decisions that have to agree, and splitting them across two files is how they
// would stop agreeing.
//
// Why a field rather than a Set Variable node: the value exists either way, and
// a field on the node that produced it adds no node type, no handler, and no
// second place to look when asking where a value came from. A Set Variable node
// earns its place when something wants to write a value nothing produced.

package backend

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// The Branch node's two output handles. A handler returns one of these and the
// walk (run.follow) takes the single edge drawn from that handle; a handle with
// no edge ends that path, which is how "if X, we are done" is drawn today.
//
// They are the words "true" and "false" rather than out-1 / out-2 so that the
// saved edge says what it means, and so the node's UI and the file agree with
// each other without a lookup table in between.
const (
	branchTrue  = "true"
	branchFalse = "false"
)

// The comparison operators a Branch node can be configured with. This is the
// whole condition language, deliberately: a small fixed set of comparisons in
// the node's own UI, rather than an expression to parse. Generalising is best
// done once one real condition exists to generalise from, and adding an
// operator later is a smaller change than taking an expression language back
// out.
const (
	opEquals      = "equals"
	opNotEquals   = "notEquals"
	opGreaterThan = "greaterThan"
	opLessThan    = "lessThan"
	opContains    = "contains"
	opIsSet       = "isSet"
)

// The suffixes a position is stored under. A Mouse Move node asked to store its
// result as "target" writes "target.x" and "target.y".
//
// The run state holds one value per name and a position is two numbers, so it
// has to become two entries under a convention. A single packed string - "800,
// 600" - was rejected: nothing can compare it, so every condition that wanted
// one of the two numbers would have to take the string apart again, and the
// taking-apart would then be a second piece of syntax nobody wrote down. Two
// numbers stay numbers, and greaterThan on them means what it says.
//
// Stated once, here, and used by storePosition. A node's UI may show the two
// names it will write; nothing else should be spelling these out.
const (
	positionSuffixX = ".x"
	positionSuffixY = ".y"
)

// resultName returns the name a node's payload asks its result to be stored
// under, and false when it asks for nothing.
//
// Absent, not a string, or empty all mean "store nothing" - the field is
// optional and an empty text input is how a user turns it off. Surrounding
// whitespace is trimmed, here and in the Branch node's variable field, so that
// a name typed with a trailing space still finds the value it stored.
func resultName(data map[string]interface{}) (string, bool) {
	name, ok := data["storeResultAs"].(string)
	if !ok {
		return "", false
	}
	name = strings.TrimSpace(name)
	return name, name != ""
}

// storeResult records a node's result under the name its payload asked for, and
// does nothing when it asked for none.
//
// "Does nothing" is the load-bearing half. A node that stored nothing must
// leave the name unset rather than write an empty value into it, because unset
// is what isSet answers about and what every other operator reads as false.
func storeResult(state *runState, data map[string]interface{}, value any) {
	name, ok := resultName(data)
	if !ok {
		return
	}
	state.set(name, value)
}

// storePosition records a position under the .x / .y convention above, as two
// numbers.
//
// float64 rather than int, because that is what a number in a saved payload
// decodes to and the comparison table therefore has one numeric type to reason
// about rather than two.
func storePosition(state *runState, data map[string]interface{}, x, y float64) {
	name, ok := resultName(data)
	if !ok {
		return
	}
	state.set(name+positionSuffixX, x)
	state.set(name+positionSuffixY, y)
}

// executeBranchTask evaluates the node's condition and reports which output the
// token leaves by: "true" or "false", and never "".
//
// The walk does the rest - one named output takes the one edge on that handle -
// so this handler is the condition and nothing else. There is no context to
// consult: it reads a map and compares two values, with nothing in between for
// a cancellation to interrupt.
func executeBranchTask(_ context.Context, task Task, state *runState, app *App) (string, error) {
	// Each read is optional at this level and checked by conditionHolds
	// instead, so that one place decides what a malformed condition is.
	variable, _ := task.Data["variable"].(string)
	operator, _ := task.Data["operator"].(string)
	value := task.Data["value"]

	holds, err := conditionHolds(state, variable, operator, value)
	if err != nil {
		// A payload this handler cannot mean, reported the way every other
		// handler reports one: the event for the status panel, the same value
		// back to the caller. Emphatically not a silently chosen branch - an
		// operator the node's own UI cannot produce means the file is wrong,
		// and picking "false" for it would run half a macro on a guess.
		//
		// endPathHandle rather than "", which is what this used to return and
		// which did the opposite of what the paragraph above says: "" means "the
		// only output" and takes *every* outgoing edge, so a branch that could
		// not decide ran both of them - clicking Buy *and* Cancel rather than
		// neither. Not choosing has to mean the token stops here.
		log.Printf("BranchNode error: %s for task %s", err, task.ID)
		app.emitEvent("task-error", map[string]interface{}{
			"taskID": task.ID,
			"error":  err.Error(),
		})
		return endPathHandle, err
	}

	branch := branchFalse
	if holds {
		branch = branchTrue
	}

	log.Printf("BranchNode %s: %q %s %v is %s", task.ID, variable, operator, value, branch)
	app.emitEvent("task-success", map[string]interface{}{
		"taskID": task.ID,
		"type":   "BranchNode",
	})
	return branch, nil
}

// conditionHolds evaluates one Branch node's condition. The error is a
// malformed condition, not a false one.
//
// The rules, each of which is a decision rather than something that fell out of
// the implementation:
//
//   - **An unset variable makes the condition false**, for every operator
//     except isSet. It is not an error: "did that click work?" is a question a
//     macro is entitled to ask before anything has answered it, and the answer
//     before anything has is no. isSet is how a macro asks the question
//     directly, and is the only operator that looks at a name nothing wrote to.
//   - **If both sides parse as numbers, they are compared numerically;
//     otherwise as strings.** That is what makes greaterThan mean what a user
//     expects of a stored count while still working on text - and it is why
//     "3" and 3 are the same value here, since one arrives from a payload and
//     the other from a node that stored a number.
//   - **contains is a substring test on the string forms**, always, even when
//     both sides are numbers. "Does 1024 contain 02" is a strange question, but
//     it is the only reading of contains that does not depend on how the value
//     got into the run state.
//   - **greaterThan and lessThan on two non-numeric strings are lexicographic**
//   - Go's own string comparison, byte by byte, so case matters and "Z" sorts
//     before "a". Defined here rather than left to fall out of the
//     implementation, because a user who compares two words deserves an answer
//     someone chose.
//   - **An unknown operator is a malformed payload.** The node's UI offers six
//     operators, so a seventh is a file that could not have been drawn, and
//     guessing a branch for it would run a macro the user never wrote.
func conditionHolds(state *runState, variable, operator string, value any) (bool, error) {
	// The operator is checked before the variable is read, so an unknown one is
	// reported whether or not the name happens to be set.
	switch operator {
	case opEquals, opNotEquals, opGreaterThan, opLessThan, opContains, opIsSet:
	default:
		return false, fmt.Errorf("unknown branch operator %q: a branch compares with equals, notEquals, greaterThan, lessThan, contains or isSet", operator)
	}

	stored, isSet := state.value(strings.TrimSpace(variable))
	if operator == opIsSet {
		return isSet, nil
	}
	if !isSet {
		return false, nil
	}

	left, right := asString(stored), asString(value)
	if operator == opContains {
		return strings.Contains(left, right), nil
	}

	if leftNumber, leftIsNumber := asNumber(stored); leftIsNumber {
		if rightNumber, rightIsNumber := asNumber(value); rightIsNumber {
			return compareNumbers(operator, leftNumber, rightNumber)
		}
	}
	return compareStrings(operator, left, right)
}

// compareNumbers answers the four ordering operators for two numbers.
//
// The default is unreachable - conditionHolds has already refused an unknown
// operator, and isSet and contains never reach here - and returns an error
// rather than false so that an operator added to the list above without a case
// here reports itself instead of quietly meaning "no".
func compareNumbers(operator string, left, right float64) (bool, error) {
	switch operator {
	case opEquals:
		return left == right, nil
	case opNotEquals:
		return left != right, nil
	case opGreaterThan:
		return left > right, nil
	case opLessThan:
		return left < right, nil
	default:
		return false, fmt.Errorf("branch operator %q has no numeric comparison", operator)
	}
}

// compareStrings is compareNumbers for two values that are not both numbers.
// The ordering is Go's, which is byte by byte and therefore case-sensitive.
func compareStrings(operator, left, right string) (bool, error) {
	switch operator {
	case opEquals:
		return left == right, nil
	case opNotEquals:
		return left != right, nil
	case opGreaterThan:
		return left > right, nil
	case opLessThan:
		return left < right, nil
	default:
		return false, fmt.Errorf("branch operator %q has no string comparison", operator)
	}
}

// asNumber reports a value's numeric form, and false if it has none.
//
// A string that parses is a number, which is what makes "3" and 3 compare
// equal: a value typed into the node's own field arrives as whichever of the
// two the frontend sent, and a user who typed 3 into a text box did not mean
// something different by it.
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		// Nothing in a decoded payload is an int - encoding/json makes every
		// number a float64 - but a test, or a future caller that stores a count
		// it computed, can put one here.
		return float64(v), true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return number, err == nil
	default:
		return 0, false
	}
}

// asString is a value's string form, for the comparisons that are not numeric.
//
// A number formats with 'f' and no fixed precision, so a stored count of 3
// reads back as "3" rather than "3.000000" - the spelling a user would have
// typed into the value field, which is what it will be compared against.
//
// A value that is neither - a payload with a nested object in it, which the
// node's UI cannot produce - formats with %v rather than failing. There is no
// sensible comparison to make of one, and reporting a malformed payload for a
// field the user cannot get wrong would be a refusal nobody could act on.
func asString(value any) string {
	switch v := value.(type) {
	case nil:
		// An absent value field. Comparing against the empty string is the
		// reading that makes `equals ""` expressible at all.
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}
