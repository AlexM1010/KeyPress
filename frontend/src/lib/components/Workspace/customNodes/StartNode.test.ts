// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import StartNode from './StartNode.svelte';
import type { StartNodeData } from '$lib/stores/flow.svelte';

/**
 * Records a hotkey the way a user does: arm the node, then let the keys arrive
 * on `window` - which is where the component listens, not on anything it
 * renders, so a key pressed with the node merely on screen still reaches it.
 *
 * The modifier flags are set to match the event a real browser sends, because
 * `handleKeyUp` reads those rather than the key names it recorded, and a keyup
 * without them ends the recording.
 */
async function record(keys: { key: string; ctrlKey?: boolean; shiftKey?: boolean }[]) {
	await fireEvent.click(screen.getByRole('button', { name: 'Record' }));

	for (const key of keys) {
		await fireEvent.keyDown(window, key);
	}
}

describe('StartNode', () => {
	it('backfills newer fields into a payload saved before they existed', () => {
		// The shape `defaultNodes` seeds a brand-new flow with: presentation only,
		// no hotkey. The backfill has to reach it in place, or `data.macroKeys`
		// would be `undefined` and the `.join('+')` in the template would throw
		// before the node painted anything.
		const { data } = renderNode(StartNode, {
			label: 'Start',
			icon: 'Play',
			color: 'bg-gradient-to-r from-blue-500 to-blue-600'
		} as unknown as StartNodeData);

		expect(data.macroKeys).toEqual([]);
		// The presentational fields a saved node carries are not the default's to
		// overwrite - they survive alongside the field that was added.
		expect(data.label).toBe('Start');
	});

	it('leaves a saved hotkey alone when backfilling', () => {
		const { data } = renderNode(StartNode, { macroKeys: ['Control', 'Shift', 'r'] });

		// The whole point of doing the backfill once at init rather than in a
		// reactive statement: a reactive one would re-run and put the empty
		// default back over the user's saved hotkey.
		expect(persistedData(data, 'StartNode')).toEqual({ macroKeys: ['Control', 'Shift', 'r'] });
	});

	it('records pressed keys into the payload in the order they went down', async () => {
		const { data } = renderNode(StartNode, { macroKeys: [] });

		await record([
			{ key: 'Control', ctrlKey: true },
			{ key: 'Shift', ctrlKey: true, shiftKey: true },
			{ key: 'k', ctrlKey: true, shiftKey: true }
		]);

		// Order is the hotkey: the backend arms "Ctrl then Shift then K", so an
		// unordered set would be a different chord from the one the user pressed.
		expect(data.macroKeys).toEqual(['Control', 'Shift', 'k']);

		const persisted = persistedData(data, 'StartNode').macroKeys as unknown[];

		// The Go side ranges over this as `[]string`, so an array of plain strings
		// is the contract. JSON would carry a number or a nested array here just
		// as happily, and the mistake would only surface once the trigger was
		// being registered against the user's real keyboard.
		expect(Array.isArray(persisted)).toBe(true);
		expect(persisted.every((key) => typeof key === 'string')).toBe(true);
		expect(persisted).toEqual(['Control', 'Shift', 'k']);
	});

	it('ignores keystrokes typed while it is not recording', async () => {
		const { data } = renderNode(StartNode, { macroKeys: ['F5'] });

		// The listeners sit on `window` for the whole life of the node, so every
		// keystroke anywhere in the app reaches them. Only the `isRecording` guard
		// stops a user typing a macro's name from rewriting its trigger.
		await fireEvent.keyDown(window, { key: 'a' });

		expect(data.macroKeys).toEqual(['F5']);
	});

	it('does not append a key that is already in the hotkey', async () => {
		const { data } = renderNode(StartNode, { macroKeys: [] });

		// Holding a key down makes the OS repeat keydown indefinitely, so without
		// the dedupe a chord held for a second would persist as hundreds of
		// entries and no key combination would ever match it again.
		await record([
			{ key: 'Control', ctrlKey: true },
			{ key: 'Control', ctrlKey: true },
			{ key: 'a', ctrlKey: true }
		]);

		expect(data.macroKeys).toEqual(['Control', 'a']);
	});

	it('seeds a recording with the special keys selected on the node', async () => {
		const { data } = renderNode(StartNode, { macroKeys: [] });

		// Ctrl and Shift are the two modifiers in every OS's key list, so this
		// holds whichever platform `detectOS` reads out of the user agent.
		await fireEvent.click(screen.getByText('Ctrl'));
		await fireEvent.click(screen.getByText('Shift'));
		await record([{ key: 'j', ctrlKey: true, shiftKey: true }]);

		// The selected modifiers come first and in the order they were toggled -
		// they are the start of the chord, not a set bolted on afterwards. They
		// are also how a user records a trigger involving a modifier their own
		// keyboard would otherwise send to the app before the node saw it.
		expect(data.macroKeys).toEqual(['Control', 'Shift', 'j']);
	});

	it('replaces the previous hotkey when a new recording starts', async () => {
		const { data } = renderNode(StartNode, { macroKeys: ['Alt', 'q'] });

		// Arming the node clears what was there rather than appending to it.
		// Otherwise re-recording could only ever grow the chord and the user would
		// have no way to shorten one.
		await record([{ key: 'z' }]);

		expect(data.macroKeys).toEqual(['z']);
	});

	it('keeps recording while a modifier is still held', async () => {
		const { data } = renderNode(StartNode, { macroKeys: [] });

		await record([
			{ key: 'Control', ctrlKey: true },
			{ key: 'a', ctrlKey: true }
		]);
		// `a` released with Ctrl still down. The user is mid-chord, and the keyup
		// handler reads the event's modifier flags rather than the keys it has
		// recorded precisely so this does not end the recording.
		await fireEvent.keyUp(window, { key: 'a', ctrlKey: true });
		await fireEvent.keyDown(window, { key: 'b', ctrlKey: true });

		expect(data.macroKeys).toEqual(['Control', 'a', 'b']);
	});

	it('stops recording once the last key is released', async () => {
		const { data } = renderNode(StartNode, { macroKeys: [] });

		await record([
			{ key: 'Control', ctrlKey: true },
			{ key: 'a', ctrlKey: true }
		]);
		// Ctrl released too, so no modifier flag is set and the chord is finished.
		await fireEvent.keyUp(window, { key: 'a' });
		await fireEvent.keyDown(window, { key: 'ignored' });

		// The keystroke after the release is somebody else's - if it landed here,
		// the node would keep swallowing the user's typing for the rest of the
		// session, since the listeners are never removed until it unmounts.
		expect(data.macroKeys).toEqual(['Control', 'a']);
		// And the button offers a fresh recording again, which is the only signal
		// the user gets that the chord was captured.
		expect(screen.getByRole('button', { name: 'Record' })).toBeDefined();
	});

	it('shows the recorded chord joined with plus signs', async () => {
		renderNode(StartNode, { macroKeys: [] });

		expect(screen.getByText('No macro')).toBeDefined();

		await record([
			{ key: 'Control', ctrlKey: true },
			{ key: 'k', ctrlKey: true }
		]);

		// Reading the chord back off the node is how the user confirms they
		// recorded what they meant to, so it has to follow the payload rather than
		// a copy taken when the node mounted.
		expect(screen.getByText('Control+k')).toBeDefined();
	});
});
