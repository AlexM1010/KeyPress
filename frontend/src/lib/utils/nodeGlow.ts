// frontend\src\lib\utils\nodeGlow.ts
//
// The run marks the workspace canvas paints onto its nodes: the orange mark on
// the nodes a run skipped, and the coloured halo on the ones it is executing
// right now. Both are generated stylesheets rather than classes written onto
// the nodes - see the comments in `Flow.svelte`, which owns the id lists these
// builders are handed and the `<svelte:head>` elements that carry their output.

/**
 * The glow colour a node shows while it is running, keyed by the same node
 * type as `NODE_TYPE_TITLES`.
 *
 * Each value is the hue of that node's own header gradient - Start, Delay and
 * Sequence are blue, the two mouse nodes green, Keypress orange, Wait For Color
 * indigo, Branch purple, the two halves of a Loop teal (both, because they are
 * one construct and a run crossing between them should not change colour) - so a
 * running card lights up in its own colour rather
 * than in one shared highlight. The gradients themselves are Tailwind classes on the node
 * components (`export let color`), which is no use as a shadow colour, so the
 * hues are named here as the `--node-glow-*` custom properties `index.scss`
 * defines. As with the titles in `nodeLabels.ts`, the node components are the
 * source of truth: when one changes colour, this table follows.
 */
export const NODE_TYPE_GLOWS: Record<string, string> = {
	StartNode: '--node-glow-blue',
	DelayNode: '--node-glow-blue',
	SequenceNode: '--node-glow-blue',
	MouseClickNode: '--node-glow-green',
	MouseMoveNode: '--node-glow-green',
	ColorPickerNode: '--node-glow-indigo',
	KeyPressNode: '--node-glow-orange',
	BranchNode: '--node-glow-purple',
	LoopStartNode: '--node-glow-teal',
	LoopEndNode: '--node-glow-teal'
};

/** For a node type that is not in the table above. */
export const DEFAULT_NODE_GLOW = '--node-glow-neutral';

/**
 * Node ids the app generates are `crypto.randomUUID()` values, but ids also
 * arrive off disk from saved macros, so nothing guarantees their shape. Only
 * ids built from characters that are inert inside an attribute-selector
 * string become selectors; anything else is dropped, so the generated
 * stylesheet can never be anything but the CSS intended here. A dropped id
 * merely loses its glow - the status message still names it.
 */
export const SELECTOR_SAFE_NODE_ID = /^[A-Za-z0-9._:-]+$/;

/**
 * CSS marking the skipped nodes, or "" when there are none. It is the *body*
 * of a stylesheet: the `<style>` element that carries it is in the markup of
 * `Flow.svelte`, because a literal `<style>` string in that component's script
 * block would be mistaken for the component's own style block by the
 * preprocessor.
 *
 * Scoped to `.flow-container` rather than left bare because a stylesheet in
 * `<head>` is global: confining it to a canvas keeps it from reaching any
 * other element that happens to carry a matching `data-id`.
 *
 * `--node-skipped-glow` is consumed by the `.svelte-flow__node` rule in
 * FlowStyle.css, which owns what the mark looks like.
 */
export function buildSkippedGlowCss(nodeIds: string[]): string {
	const selectors = nodeIds
		.filter((id) => SELECTOR_SAFE_NODE_ID.test(id))
		.map((id) => `.flow-container .svelte-flow__node[data-id="${id}"]`);
	if (selectors.length === 0) return '';
	return (
		`${selectors.join(',')}{--node-skipped-glow:` +
		`0 0 0 2px var(--skipped-glow),0 0 22px 6px var(--skipped-glow-soft)}`
	);
}

/**
 * CSS lighting up the nodes that are executing, or "" when none are.
 *
 * Shaped exactly like `buildSkippedGlowCss` - same `.flow-container` scope,
 * same id filter, same handing-off to FlowStyle.css through a custom property
 * - but grouped by colour rather than emitted per id, because most runs light
 * one or two nodes of a handful of colours and a selector list per colour is
 * the smaller stylesheet.
 *
 * The glow is a halo with no solid ring, which is what distinguishes it from
 * the skipped mark; see the rule in FlowStyle.css.
 *
 * `glows` is a parameter rather than a read of `nodeGlows`, so the reactive
 * statement in `Flow.svelte` rebuilds the stylesheet when a new run recolours
 * the graph and not only when the set of lit ids changes.
 */
export function buildActiveGlowCss(nodeIds: string[], glows: Map<string, string>): string {
	const byGlow = new Map<string, string[]>();
	for (const id of nodeIds) {
		if (!SELECTOR_SAFE_NODE_ID.test(id)) continue;
		const glow = glows.get(id) ?? DEFAULT_NODE_GLOW;
		const selectors = byGlow.get(glow);
		const selector = `.flow-container .svelte-flow__node[data-id="${id}"]`;
		if (selectors) selectors.push(selector);
		else byGlow.set(glow, [selector]);
	}

	let css = '';
	for (const [glow, selectors] of byGlow) {
		css += `${selectors.join(',')}{--node-active-glow:` + `0 0 26px 8px var(${glow})}`;
	}
	return css;
}
