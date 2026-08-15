// @vitest-environment jsdom
import { describe, expect, it } from 'vitest';
import { fireEvent, screen } from '@testing-library/svelte';
import { persistedData, renderNode } from '$lib/test/nodeHarness';
import MouseClickNode from './MouseClickNode.svelte';
import type { MouseClickNodeData } from '$lib/stores/flow.svelte';

/**
 * A saved payload with every field set to something other than its default, so a
 * test that reads a value back cannot be passing on the backfill's default
 * instead of on what the component wrote.
 *
 * A function rather than a constant that gets spread, because `scrollDirection`
 * is an array and a spread would hand every test the *same* one:
 * `toggleDirection` pushes into it in place, so one test's scroll edit would
 * arrive pre-applied in the next.
 */
function savedPayload(overrides: Partial<MouseClickNodeData> = {}): MouseClickNodeData {
	return {
		buttonType: 'middle',
		numberOfClicks: 4,
		clickDelay: 250,
		pressReleaseDelay: 40,
		releaseAfterPress: false,
		scrollDirection: ['Horizontal'],
		scrollLines: 3,
		...overrides
	};
}

/** Scrolling lives behind a collapsed section, so nothing in it exists until this runs. */
async function openScrollOptions() {
	await fireEvent.click(screen.getByRole('button', { name: /Scroll Options/ }));
}

describe('MouseClickNode', () => {
	it('backfills newer fields into a payload saved before they existed', () => {
		// A macro saved when the node was a button and a count. The backfill has to
		// reach it in place, or `data.scrollDirection.includes(...)` in the template
		// would throw the moment the user opened Scroll Options.
		const { data } = renderNode(MouseClickNode, {
			buttonType: 'right',
			numberOfClicks: 3
		} as MouseClickNodeData);

		expect(data).toEqual({
			buttonType: 'right',
			numberOfClicks: 3,
			clickDelay: 0.1,
			pressReleaseDelay: 100,
			releaseAfterPress: true,
			scrollDirection: ['Vertical'],
			scrollLines: 0
		});
	});

	it('leaves saved values alone when backfilling', () => {
		const saved = savedPayload();
		const { data } = renderNode(MouseClickNode, savedPayload());

		// `{ ...DEFAULT_DATA, ...data }` and not the other way round: with the
		// spreads swapped this node would open every saved macro as a single left
		// click, and saving it back would make that permanent.
		expect(persistedData(data, 'MouseClickNode')).toEqual(saved);
	});

	it('writes the button type as the lowercase name the backend matches on', async () => {
		const { data } = renderNode(MouseClickNode, savedPayload({ buttonType: 'left' }));

		// The buttons are labelled "Left"/"Middle"/"Right" for the user, but Go
		// compares the string to "left"/"middle"/"right" and refuses anything else
		// outright (`Unsupported buttonType`), so the capitalisation is the
		// contract and not presentation.
		await fireEvent.click(screen.getByRole('button', { name: 'Right' }));
		expect(data.buttonType).toBe('right');

		await fireEvent.click(screen.getByRole('button', { name: 'Middle' }));
		expect(persistedData(data, 'MouseClickNode').buttonType).toBe('middle');
	});

	it('keeps the click count a number and reveals the between-clicks delay', async () => {
		const { data } = renderNode(
			MouseClickNode,
			savedPayload({ numberOfClicks: 1, releaseAfterPress: false })
		);

		// One click has nothing to wait between, so the delay is not on screen yet.
		expect(screen.queryByLabelText('delay')).toBeNull();

		await fireEvent.input(screen.getByLabelText('Clicks'), { target: { value: '5' } });

		// The type is the assertion that matters: Go reads this as
		// `task.Data["numberOfClicks"].(float64)` and abandons the task when it is
		// anything else, so a count that arrived as a string is a macro that
		// silently stops clicking.
		expect(data.numberOfClicks).toBe(5);
		expect(typeof persistedData(data, 'MouseClickNode').numberOfClicks).toBe('number');
		// And the count crossing 1 is what mounts the delay control, so a break in
		// that condition would leave the user no way to space the clicks out.
		expect(screen.getByLabelText('delay')).toBeDefined();
	});

	it('stores the between-clicks delay in milliseconds whatever unit the box shows', async () => {
		const { data } = renderNode(
			MouseClickNode,
			savedPayload({ numberOfClicks: 4, releaseAfterPress: false })
		);

		// The box starts in seconds while the payload is in milliseconds - Go does
		// `time.Duration(clickDelay) * time.Millisecond` - so half a second typed
		// here has to land as 500 and not as 0.5, which would be half a
		// *microsecond* of pause and read as no pause at all.
		await fireEvent.input(screen.getByLabelText('delay'), { target: { value: '0.5' } });

		expect(data.clickDelay).toBe(500);
		expect(persistedData(data, 'MouseClickNode').clickDelay).toBe(500);
	});

	it('keeps releaseAfterPress a boolean and hides the hold time when it is off', async () => {
		const { data } = renderNode(MouseClickNode, savedPayload({ releaseAfterPress: true }));

		await fireEvent.click(screen.getByLabelText('Release'));

		// `task.Data["releaseAfterPress"].(bool)` on the Go side, so the falsy
		// stand-ins a checkbox can produce ("", "off", 0) all fail the assertion
		// and leave the button held down.
		expect(data.releaseAfterPress).toBe(false);
		expect(typeof persistedData(data, 'MouseClickNode').releaseAfterPress).toBe('boolean');
		// Nothing is held, so there is no hold time to set.
		expect(screen.queryByLabelText('after')).toBeNull();
	});

	it('stores the press-and-hold time in milliseconds', async () => {
		const { data } = renderNode(
			MouseClickNode,
			savedPayload({ numberOfClicks: 1, releaseAfterPress: true })
		);

		await fireEvent.input(screen.getByLabelText('after'), { target: { value: '0.25' } });

		// Same seconds-in, milliseconds-stored conversion as the click delay, and
		// the same consequence if it slips: this one decides how long the mouse
		// button stays down on the user's actual desktop.
		expect(data.pressReleaseDelay).toBe(250);
		expect(persistedData(data, 'MouseClickNode').pressReleaseDelay).toBe(250);
	});

	it('empties the scroll direction set to an array and not to a sentinel', async () => {
		const { data } = renderNode(MouseClickNode, savedPayload({ scrollDirection: ['Vertical'] }));

		await openScrollOptions();
		await fireEvent.click(screen.getByRole('button', { name: 'Vertical' }));

		expect(data.scrollDirection).toEqual([]);

		const persisted = persistedData(data, 'MouseClickNode').scrollDirection;

		// Go reads this as `[]interface{}` and ranges over it. An empty *array*
		// simply scrolls nowhere; "" or null fails the type assertion, and the
		// clicking half of the node would go down with it if that ever became an
		// error rather than the silent skip it is today.
		expect(Array.isArray(persisted)).toBe(true);
		expect(persisted).toEqual([]);
	});

	it('collects both scroll directions rather than replacing one with the other', async () => {
		const { data } = renderNode(MouseClickNode, savedPayload({ scrollDirection: ['Vertical'] }));

		await openScrollOptions();
		await fireEvent.click(screen.getByRole('button', { name: 'Horizontal' }));

		// It is a set, not a choice: the Go loop scrolls once per direction, so
		// both entries have to survive - and as plain strings, since a direction
		// that is not exactly "Vertical" or "Horizontal" falls through the switch
		// and scrolls nothing.
		expect(persistedData(data, 'MouseClickNode').scrollDirection).toEqual([
			'Vertical',
			'Horizontal'
		]);
	});

	it('keeps the scroll distance a number, negatives included', async () => {
		const { data } = renderNode(MouseClickNode, savedPayload({ scrollLines: 3 }));

		// The whole section is collapsed on mount, so this control does not exist
		// until the toggle has run - which is what makes the toggle worth pinning.
		expect(screen.queryByLabelText('Lines')).toBeNull();
		await openScrollOptions();

		await fireEvent.input(screen.getByLabelText('Lines'), { target: { value: '-12' } });

		// `task.Data["scrollLines"].(float64)`, and the sign is what the direction
		// of travel is encoded in - so a string here is not a slower scroll, it is
		// no scroll. (The Go side additionally gates scrolling on `scrollLines > 0`
		// today, which is a backend question; the payload's job is to carry the
		// number the user typed.)
		expect(data.scrollLines).toBe(-12);
		expect(persistedData(data, 'MouseClickNode').scrollLines).toBe(-12);
	});
});
