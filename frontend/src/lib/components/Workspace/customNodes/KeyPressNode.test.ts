// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { nodesData } from '$lib/stores/flow';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import KeyPressNode from './KeyPressNode.svelte';

/**
 * The payload shape, restated here because the component owns it: `KeyPressNodeData`
 * is declared inside `KeyPressNode.svelte` and deliberately not exported, the way
 * `ColorPickerNode` keeps its own. Redeclaring it is the cost of that, and a cheap
 * one - a field added there without being added here shows up as a test that no
 * longer compiles, which is the right place to notice it.
 */
type KeyPressData = {
	keyKind: 'Character' | 'Named';
	character: string;
	namedKey: string;
	keyAction: 'Press' | 'Hold' | 'Release';
	modifiers: string[];
	pressDuration: number;
};

/** A complete saved payload, so a test only has to say what it varies. */
const saved = (overrides: Partial<KeyPressData> = {}): KeyPressData => ({
	keyKind: 'Character',
	character: 'a',
	namedKey: 'enter',
	keyAction: 'Press',
	modifiers: [],
	pressDuration: 50,
	...overrides
});

describe('KeyPressNode', () => {
	it('backfills newer fields into a payload saved before they existed', () => {
		// A macro saved when the node was only a character and an action. The
		// backfill has to reach it in place: reassigning `data` would detach the
		// component from the object the flow store holds and every edit made after
		// this point would be written to a copy nobody saves.
		const { data } = renderNode(KeyPressNode, {
			keyKind: 'Character',
			character: 'z'
		} as KeyPressData);

		expect(data).toEqual({
			keyKind: 'Character',
			character: 'z',
			namedKey: 'enter',
			keyAction: 'Press',
			modifiers: [],
			pressDuration: 50
		});
	});

	it('repairs a modifiers field that a hand-edited save left as a non-array', () => {
		// `data.modifiers.includes(...)` runs on mount, and everything downstream of
		// it - the checkboxes, `syncModifiers`, the saved file - assumes an array.
		// A string survives `Object.assign` (it is a value, so it is *kept*), so the
		// repair is a separate check and this is what proves it still runs.
		const { data } = renderNode(KeyPressNode, {
			...saved(),
			modifiers: 'Ctrl'
		} as unknown as KeyPressData);

		expect(Array.isArray(data.modifiers)).toBe(true);
		expect(data.modifiers).toEqual([]);
	});

	it('stores "Named" for the kind the buttons call "Special"', async () => {
		const { data } = renderNode(KeyPressNode, saved());

		await fireEvent.click(screen.getByRole('button', { name: 'Special' }));

		// The label and the stored value differ on purpose, and that is the whole
		// point of this test: the Go handler accepts `Named` and nothing else, so a
		// rename that followed through to the payload would make every saved
		// Keypress node unreadable to it. The button face is allowed to move; the
		// word in the file is not.
		expect(data.keyKind).toBe('Named');
		expect(persistedData(data, 'KeyPressNode').keyKind).toBe('Named');
		expect(screen.queryByRole('button', { name: 'Named' })).toBeNull();
	});

	it('writes a single typed character into the payload', async () => {
		const { data } = renderNode(KeyPressNode, saved());

		const box = screen.getByLabelText('Character') as HTMLInputElement;

		// One keystroke per node, so one character per box. What is stored is the
		// character itself and never the keystroke that produces it - `!` is Shift+1
		// on a UK keyboard and unshifted on a French one, and the Go handler works
		// that out at run time against whatever layout is in force.
		expect(box.maxLength).toBe(1);

		await fireEvent.input(box, { target: { value: '!' } });

		expect(data.character).toBe('!');
		expect(persistedData(data, 'KeyPressNode').character).toBe('!');
		// The character kind has no key name to pick, so the dropdown is not there.
		expect(screen.queryByLabelText('Special Key')).toBeNull();
	});

	it('writes the chosen special key into the payload', async () => {
		const { data } = renderNode(KeyPressNode, saved({ keyKind: 'Named' }));

		await fireEvent.change(screen.getByLabelText('Special Key'), {
			target: { value: 'f5' }
		});

		// These are robotgo's key names verbatim - the string picked here is the
		// string `actions_keyboard.go` hands to robotgo, so a prettier display name
		// would be a translation table whose drift shows up only as a keystroke that
		// silently does nothing.
		expect(data.namedKey).toBe('f5');
		expect(persistedData(data, 'KeyPressNode').namedKey).toBe('f5');
	});

	it('still offers a special key that has since left the list', () => {
		// A macro saved before the keypad keys were dropped from `namedKeys`. The
		// backend still runs `num5`, so the node has to keep showing it: with no
		// matching option the `<select>` renders *blank* and the user's choice looks
		// lost while remaining the thing that actually runs.
		const { data } = renderNode(KeyPressNode, saved({ keyKind: 'Named', namedKey: 'num5' }));

		const select = screen.getByLabelText('Special Key') as HTMLSelectElement;
		const options = [...select.options].map((option) => option.value);

		expect(options).toContain('num5');
		expect(select.value).toBe('num5');
		// Nothing was rewritten on the way through - the payload still names the key
		// it was saved with.
		expect(data.namedKey).toBe('num5');
	});

	it('offers a press duration only for the action that has one', async () => {
		const { data } = renderNode(KeyPressNode, saved({ keyAction: 'Press' }));

		expect(screen.getByLabelText('Press Duration')).toBeDefined();

		await fireEvent.click(screen.getByRole('button', { name: 'Hold' }));

		// `Hold` and `Release` are instantaneous toggles that leave the key down for
		// a later node to deal with, so there is nothing to hold it *for*. The saved
		// duration survives the trip so flipping back and forth does not cost the
		// user the value they typed.
		expect(data.keyAction).toBe('Hold');
		expect(screen.queryByLabelText('Press Duration')).toBeNull();
		expect(data.pressDuration).toBe(50);
	});

	it('writes a press duration back as a number of milliseconds', async () => {
		const { data } = renderNode(KeyPressNode, saved());

		await fireEvent.input(screen.getByLabelText('Press Duration'), {
			target: { value: '250' }
		});

		// The type is the assertion, not the value: the Go side reads this with an
		// unchecked `.(float64)`, which a string fails at run time with the user's
		// real keyboard already in play - having passed every compile-time check on
		// the way there.
		expect(data.pressDuration).toBe(250);
		expect(persistedData(data, 'KeyPressNode').pressDuration).toBe(250);
	});

	it('writes ticked modifiers in a fixed order, whatever order they were ticked in', async () => {
		const { data } = renderNode(KeyPressNode, saved());

		await fireEvent.click(screen.getByLabelText('Shift'));
		await fireEvent.click(screen.getByLabelText('Ctrl'));

		// Ctrl, Alt, Shift, Windows regardless of what the user did first. The order
		// is not cosmetic: the saved array is compared against the file to answer
		// "are there unsaved changes", so an array that churned with tick order
		// would report a macro as dirty for a set it already holds.
		expect(data.modifiers).toEqual(['Ctrl', 'Shift']);
	});

	it('drops a modifier when its box is unticked', async () => {
		const { data } = renderNode(KeyPressNode, saved({ modifiers: ['Ctrl', 'Shift'] }));

		await fireEvent.click(screen.getByLabelText('Shift'));

		expect(data.modifiers).toEqual(['Ctrl']);
	});

	it('leaves no modifiers rather than a "None" sentinel', async () => {
		const { data } = renderNode(KeyPressNode, saved({ modifiers: ['Ctrl'] }));

		await fireEvent.click(screen.getByLabelText('Ctrl'));

		// `modifiers` is a set, and the empty set is spelled `[]`. A sentinel value
		// would have to be understood by the Go handler as well, which merges this
		// set with whatever the layout itself needs to reach the character.
		expect(data.modifiers).toEqual([]);
	});

	it('persists the modifiers as an array', async () => {
		const { data } = renderNode(KeyPressNode, saved());

		await fireEvent.click(screen.getByLabelText('Windows'));

		// Through the real `serializeMacro`, because that string is what reaches the
		// file: the Go handler ranges over this field, and anything that arrived as
		// a bare string would be ranged over as characters.
		const modifiers = persistedData(data, 'KeyPressNode').modifiers;
		expect(Array.isArray(modifiers)).toBe(true);
		expect(modifiers).toEqual(['Windows']);
	});

	it('announces a ticked modifier only once it has reached the payload', async () => {
		const data = saved();

		// The workspace answers "does this differ from the file?" by re-serialising
		// the graph whenever the node store announces an edit, so an announcement
		// that goes out *before* the write is worth nothing - it reports the payload
		// as it was.
		nodesData.set([{ id: 'test-node-1', type: 'KeyPressNode', position: { x: 0, y: 0 }, data }]);

		const announced: string[][] = [];
		const stopListening = nodesData.subscribe((nodes) => {
			announced.push([...((nodes[0].data as KeyPressData).modifiers ?? [])]);
		});

		renderNode(KeyPressNode, data);
		await fireEvent.click(screen.getByLabelText('Ctrl'));
		stopListening();

		// This is the safety net for a documented ordering hazard: `syncModifiers`
		// writes `data.modifiers` from inside a reactive statement, the compiler
		// cannot see that through the function call, and Svelte then runs the
		// reactive blocks in source order. With the `markGraphEdited` block above
		// `syncModifiers` the tick lands after the announcement has gone out and
		// produces *zero* notifications carrying it - the macro looks saved while
		// the file has no Ctrl in it. Asserted through the store rather than by
		// counting calls, so it still means the same thing after the Svelte 5
		// migration rewrites how the ordering is expressed.
		expect(announced.at(-1)).toEqual(['Ctrl']);
	});
});
