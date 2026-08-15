// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import MouseMoveNode from './MouseMoveNode.svelte';

type Coordinates = { x: number; y: number };

/**
 * The node's payload, mirrored from the component that owns it.
 *
 * `MouseMoveNode.svelte` declares this type inside itself and exports nothing -
 * the same arrangement `KeyPressNode` has, and the reason `stores/flow.ts` does
 * not carry every payload shape. Writing it out here is what lets a test say
 * `data.speed.value` and be told at compile time when the component's shape
 * moves out from under it.
 */
type MouseMoveData = {
	startPosition: { type: 'Mouse' | 'Fixed'; coordinates: Coordinates };
	endPosition: { type: 'Mouse' | 'Fixed'; coordinates: Coordinates };
	dragWhileMoving: boolean;
	speed: { type: 'Instant' | 'Human'; value: number; randomize: boolean; variance: number };
	pathType: 'Straight' | 'Human';
	customPath: Coordinates[];
};

/**
 * A saved payload with every field set away from its default, so a value read
 * back cannot be the backfill's default wearing the same number.
 *
 * A function rather than a spread-able constant because the payload is a tree:
 * a spread would share the nested `coordinates` and `speed` objects between
 * tests, and those are exactly what the component mutates in place.
 */
function savedPayload(overrides: Partial<MouseMoveData> = {}): MouseMoveData {
	return {
		startPosition: { type: 'Fixed', coordinates: { x: 120, y: 240 } },
		endPosition: { type: 'Fixed', coordinates: { x: 800, y: 600 } },
		dragWhileMoving: true,
		speed: { type: 'Human', value: 300, randomize: true, variance: 35 },
		pathType: 'Human',
		customPath: [],
		...overrides
	};
}

/** Speed, drag and path all live behind a collapsed section and do not exist until this runs. */
async function openMovementSettings() {
	await fireEvent.click(screen.getByRole('button', { name: /Movement Settings/ }));
}

/** The saved payload as the file would hold it, typed so a test can reach into the tree. */
function persisted(data: MouseMoveData): MouseMoveData {
	return persistedData(data, 'MouseMoveNode') as unknown as MouseMoveData;
}

describe('MouseMoveNode', () => {
	it('backfills a nested field a saved node is missing, not just a top-level one', () => {
		// The shape that used to slip through. A node saved before `variance`
		// existed still *has* a `speed`, so a check that only asked whether
		// `speed` was null passed and left `speed.variance` undefined - and the
		// Go handler read that field straight out of the payload, where a bare
		// type assertion turned it into a panic mid-macro. Same story for a
		// `startPosition` whose `coordinates` predate it.
		const { data } = renderNode(MouseMoveNode, {
			startPosition: { type: 'Fixed' },
			endPosition: { type: 'Fixed', coordinates: { x: 800 } },
			speed: { type: 'Human', value: 300 },
			pathType: 'Human'
		} as unknown as MouseMoveData);

		expect(data.startPosition.coordinates).toEqual({ x: 0, y: 0 });
		// The half that was there is kept; only the missing half is filled.
		expect(data.endPosition.coordinates).toEqual({ x: 800, y: 0 });
		expect(data.speed.randomize).toBe(false);
		expect(data.speed.variance).toBe(20);
		expect(data.dragWhileMoving).toBe(false);
		expect(data.customPath).toEqual([]);

		// And the whole thing survives the trip to the file as numbers, since
		// that is what the backend asserts them to be.
		const persisted = persistedData(data, 'MouseMoveNode');
		const speed = persisted.speed as MouseMoveData['speed'];
		expect(typeof speed.value).toBe('number');
		expect(typeof speed.variance).toBe('number');
	});

	it('backfills newer fields into a payload saved before they existed', () => {
		// A macro saved when the node was two coordinates and nothing else. The
		// backfill has to reach it in place, or `data.endPosition.type` in the
		// template would throw before the node painted anything.
		const { data } = renderNode(MouseMoveNode, {
			startPosition: { type: 'Fixed', coordinates: { x: 10, y: 20 } }
		} as unknown as MouseMoveData);

		expect(data).toEqual({
			startPosition: { type: 'Fixed', coordinates: { x: 10, y: 20 } },
			endPosition: { type: 'Fixed', coordinates: { x: 0, y: 0 } },
			dragWhileMoving: false,
			speed: { type: 'Instant', value: 500, randomize: false, variance: 20 },
			pathType: 'Straight',
			customPath: []
		});
	});

	it('leaves saved values alone when backfilling', async () => {
		const saved = savedPayload();
		const { data } = renderNode(MouseMoveNode, savedPayload());

		// This node backfills from a *reactive* block rather than the once-on-init
		// `Object.assign` the other nodes use, so it re-runs on every edit for as
		// long as the node is on screen. The `== null` guard on each field is the
		// only thing standing between a saved macro and the defaults being written
		// back over it - so the check is made again after an edit has fired it.
		await fireEvent.click(screen.getByRole('button', { name: /Movement Settings/ }));

		expect(persisted(data)).toEqual(saved);
	});

	it('keeps the start coordinates numbers', async () => {
		// End set to Mouse so only the start's X/Y pair is on screen and the labels
		// are unambiguous.
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ endPosition: { type: 'Mouse', coordinates: { x: 0, y: 0 } } })
		);

		await fireEvent.input(screen.getByLabelText('X'), { target: { value: '640' } });
		await fireEvent.input(screen.getByLabelText('Y'), { target: { value: '480' } });

		expect(data.startPosition.coordinates).toEqual({ x: 640, y: 480 });

		const coordinates = persisted(data).startPosition.coordinates;

		// Go reads these as `coords["x"].(float64)` with no `, ok` to catch it, so
		// a coordinate that arrived as a string does not mis-aim the pointer - it
		// panics the task mid-macro.
		expect(typeof coordinates.x).toBe('number');
		expect(typeof coordinates.y).toBe('number');
		expect(coordinates).toEqual({ x: 640, y: 480 });
	});

	it('keeps the end coordinates numbers', async () => {
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ startPosition: { type: 'Mouse', coordinates: { x: 0, y: 0 } } })
		);

		await fireEvent.input(screen.getByLabelText('X'), { target: { value: '-30' } });

		// Negative coordinates are legal - a second monitor to the left of the
		// primary one lives there - and the field's range allows them, so the sign
		// has to survive the round trip rather than being clamped away.
		expect(data.endPosition.coordinates.x).toBe(-30);
		expect(persisted(data).endPosition.coordinates.x).toBe(-30);
	});

	it('gives the start and end coordinate boxes separate identities', async () => {
		const { data } = renderNode(MouseMoveNode, savedPayload());

		// Both position pairs are on screen at once, which is precisely the case
		// `NumberInput`'s per-instance id exists for: with the old hard-coded id
		// every label in the document resolved to the first box, so typing an end
		// coordinate moved the start one instead. Two matches per label is the
		// shape that proves they are four independent inputs.
		const xInputs = screen.getAllByLabelText('X');
		const yInputs = screen.getAllByLabelText('Y');
		expect(xInputs).toHaveLength(2);
		expect(yInputs).toHaveLength(2);

		await fireEvent.input(xInputs[1], { target: { value: '999' } });
		await fireEvent.input(yInputs[1], { target: { value: '111' } });

		expect(persisted(data).endPosition.coordinates).toEqual({ x: 999, y: 111 });
		// The start pair - the one the shared id used to hijack - is untouched.
		expect(persisted(data).startPosition.coordinates).toEqual({ x: 120, y: 240 });
	});

	it('switches a position between the cursor and fixed coordinates', async () => {
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ endPosition: { type: 'Mouse', coordinates: { x: 0, y: 0 } } })
		);

		// Two groups render a "Fixed" button; the start position's is the first in
		// the document.
		await fireEvent.click(screen.getAllByRole('button', { name: 'Mouse' })[0]);
		expect(data.startPosition.type).toBe('Mouse');
		// Switching to the live cursor takes the coordinate boxes away...
		expect(screen.queryByLabelText('X')).toBeNull();

		await fireEvent.click(screen.getAllByRole('button', { name: 'Fixed' })[0]);

		// ...and switching back returns the coordinates the user typed rather than
		// zeroes, so a mis-click costs nothing. Go branches on this exact string
		// (`startPos["type"] == "Mouse"`), which is why the capitalisation is
		// asserted and not just the mode.
		expect(data.startPosition.type).toBe('Fixed');
		expect(persisted(data).startPosition.coordinates).toEqual({ x: 120, y: 240 });
	});

	it('refuses to take both endpoints from the live cursor', () => {
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ startPosition: { type: 'Mouse', coordinates: { x: 0, y: 0 } } })
		);

		// Both ends on "Mouse" resolves to the same point on the Go side - it reads
		// `robotgo.Location()` once and uses it for both - so the move would be a
		// no-op the user had no way to see was one. The end group's Mouse button is
		// disabled for as long as the start is on it.
		expect(screen.getAllByRole('button', { name: 'Mouse' })[1]).toHaveProperty('disabled', true);
		expect(data.endPosition.type).toBe('Fixed');
	});

	it('keeps the drag flag a boolean', async () => {
		const { data } = renderNode(MouseMoveNode, savedPayload({ dragWhileMoving: false }));

		// Collapsed on mount, so the checkbox does not exist until the toggle runs.
		expect(screen.queryByLabelText('Drag')).toBeNull();
		await openMovementSettings();

		await fireEvent.click(screen.getByLabelText('Drag'));

		// `task.Data["dragWhileMoving"].(bool)`, unchecked - so a checkbox that
		// persisted "on" or 1 panics the task rather than merely failing to drag,
		// and it does so while the left button may already be held down.
		expect(data.dragWhileMoving).toBe(true);
		expect(typeof persisted(data).dragWhileMoving).toBe('boolean');
	});

	it('stores a human move speed in milliseconds whatever unit the box shows', async () => {
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ speed: { type: 'Instant', value: 500, randomize: false, variance: 20 } })
		);
		await openMovementSettings();

		// Instant needs no duration, so the speed box only exists once the move is
		// a Human one. "Human" labels a button in both the speed and the path
		// group; the speed group is the first in the document.
		expect(screen.queryByLabelText('Speed')).toBeNull();
		await fireEvent.click(screen.getAllByRole('button', { name: 'Human' })[0]);
		expect(data.speed.type).toBe('Human');

		// This one starts in milliseconds, which is also what `robotgo.MouseSleep`
		// is set from - so a typed 800 is 800 and must not be scaled on the way in.
		await fireEvent.input(screen.getByLabelText('Speed'), { target: { value: '800' } });
		expect(data.speed.value).toBe(800);

		// Same box in seconds: the display changes, the stored unit does not.
		await fireEvent.click(screen.getByRole('button', { name: /Click to convert to seconds/ }));
		await fireEvent.input(screen.getByLabelText('Speed'), { target: { value: '2' } });

		expect(data.speed.value).toBe(2000);
		expect(persisted(data).speed.value).toBe(2000);
	});

	it('keeps the speed variance a number behind its own toggle', async () => {
		const { data } = renderNode(
			MouseMoveNode,
			savedPayload({ speed: { type: 'Human', value: 300, randomize: false, variance: 20 } })
		);
		await openMovementSettings();

		// The slider is only worth showing when the randomisation it feeds is on.
		expect(screen.queryByLabelText('Variance')).toBeNull();
		await fireEvent.click(screen.getByLabelText('Randomize'));

		await fireEvent.input(screen.getByLabelText('Variance'), { target: { value: '60' } });

		// Go divides this by 100 to get the fraction of the speed to jitter by
		// (`speed["variance"].(float64)`), so a string is a panic and a percentage
		// that arrived as "60%" would be one too.
		expect(data.speed.variance).toBe(60);
		expect(persisted(data).speed).toEqual({
			type: 'Human',
			value: 300,
			randomize: true,
			variance: 60
		});
	});

	it('writes the path type as the exact string the backend branches on', async () => {
		const { data } = renderNode(MouseMoveNode, savedPayload({ pathType: 'Straight' }));
		await openMovementSettings();

		// The second "Human" in the document is the path group's - see the speed
		// test above for the first.
		await fireEvent.click(screen.getAllByRole('button', { name: 'Human' })[1]);

		// `pathType := task.Data["pathType"].(string)` with no `, ok`, compared to
		// "Straight" - so the two spellings are a contract, and anything that is
		// not a string takes the whole task down with it.
		expect(data.pathType).toBe('Human');
		expect(persisted(data).pathType).toBe('Human');

		await fireEvent.click(screen.getAllByRole('button', { name: 'Straight' })[0]);
		expect(persisted(data).pathType).toBe('Straight');
	});
});
