// actions_mouse_test.go
//
// The mouse handlers themselves drive the real cursor, so what is tested here
// is the part that reads a saved node - which is where a macro actually goes
// wrong, and the part that used to panic on anything it did not expect.

package backend

import "testing"

func TestNumberInReadsAJSONNumber(t *testing.T) {
	// Everything numeric in a saved macro arrives as a float64, because that is
	// what encoding/json decodes a JSON number into. An int in the map is
	// therefore not a case that can occur off disk - and is deliberately not
	// accepted, so a test fixture that hand-writes one fails here rather than
	// passing while the real payload would not.
	data := map[string]interface{}{"x": float64(120), "y": 0.5, "int": 3, "text": "120"}

	if got, ok := numberIn(data, "x"); !ok || got != 120 {
		t.Fatalf("numberIn(x) = %v, %v; want 120, true", got, ok)
	}
	if got, ok := numberIn(data, "y"); !ok || got != 0.5 {
		t.Fatalf("numberIn(y) = %v, %v; want 0.5, true", got, ok)
	}
	if _, ok := numberIn(data, "int"); ok {
		t.Fatal("numberIn(int) accepted an int; JSON never produces one")
	}
	if _, ok := numberIn(data, "text"); ok {
		t.Fatal("numberIn(text) accepted a string")
	}
	if _, ok := numberIn(data, "absent"); ok {
		t.Fatal("numberIn(absent) reported a missing key as usable")
	}
}

// A payload in the shape the Mouse Move node saves, which each test then
// damages in one specific way.
func mouseMovePayload() map[string]interface{} {
	return map[string]interface{}{
		"speed": map[string]interface{}{
			"type":      "Human",
			"value":     float64(300),
			"randomize": true,
			"variance":  float64(35),
		},
		"pathType":        "Human",
		"dragWhileMoving": true,
	}
}

func TestReadMouseMoveSettingsReadsAWholePayload(t *testing.T) {
	settings, reason := readMouseMoveSettings(mouseMovePayload())
	if reason != "" {
		t.Fatalf("readMouseMoveSettings rejected a good payload: %s", reason)
	}

	if settings.speedType != "Human" || settings.pathType != "Human" {
		t.Fatalf("speedType/pathType = %q/%q; want Human/Human", settings.speedType, settings.pathType)
	}
	if settings.speedValue != 300 || settings.variance != 35 {
		t.Fatalf("speedValue/variance = %v/%v; want 300/35", settings.speedValue, settings.variance)
	}
	if !settings.randomize || !settings.dragWhileMoving {
		t.Fatalf("randomize/dragWhileMoving = %v/%v; want true/true", settings.randomize, settings.dragWhileMoving)
	}
}

func TestReadMouseMoveSettingsReportsWhatItCannotGuess(t *testing.T) {
	// Each of these used to be a bare type assertion, so each of these payloads
	// panicked rather than reporting anything. The message has to name the field
	// as well as refuse the run: "panic: interface conversion" told the user
	// which macro died but nothing about which field to fix.
	for _, tc := range []struct {
		name   string
		damage func(map[string]interface{})
		want   string
	}{
		{"no speed at all", func(d map[string]interface{}) { delete(d, "speed") }, "Invalid or missing speed settings"},
		{"speed of the wrong shape", func(d map[string]interface{}) { d["speed"] = "fast" }, "Invalid or missing speed settings"},
		{"no speed type", func(d map[string]interface{}) {
			delete(d["speed"].(map[string]interface{}), "type")
		}, "Invalid or missing speed type"},
		{"no speed value", func(d map[string]interface{}) {
			delete(d["speed"].(map[string]interface{}), "value")
		}, "Invalid or missing speed value"},
		{"speed value as a string", func(d map[string]interface{}) {
			d["speed"].(map[string]interface{})["value"] = "300"
		}, "Invalid or missing speed value"},
		{"no path type", func(d map[string]interface{}) { delete(d, "pathType") }, "Invalid or missing path type"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := mouseMovePayload()
			tc.damage(data)

			if _, reason := readMouseMoveSettings(data); reason != tc.want {
				t.Fatalf("reason = %q; want %q", reason, tc.want)
			}
		})
	}
}

func TestReadMouseMoveSettingsFillsInWhatItCanAssume(t *testing.T) {
	// The other half of the line: a macro saved before these fields existed is
	// still a macro that means something, so it runs rather than being refused.
	// `randomize` off makes `variance` irrelevant, and no `dragWhileMoving` is
	// plainly no drag.
	data := mouseMovePayload()
	speed := data["speed"].(map[string]interface{})
	delete(speed, "randomize")
	delete(speed, "variance")
	delete(data, "dragWhileMoving")

	settings, reason := readMouseMoveSettings(data)
	if reason != "" {
		t.Fatalf("readMouseMoveSettings refused a payload it could complete: %s", reason)
	}
	if settings.randomize {
		t.Fatal("randomize defaulted to true")
	}
	if settings.variance != 0 {
		t.Fatalf("variance = %v; want 0", settings.variance)
	}
	if settings.dragWhileMoving {
		t.Fatal("dragWhileMoving defaulted to true - a macro that predates the field would drag")
	}
}
