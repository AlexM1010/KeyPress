// frontend/src/lib/test/graphState.svelte.ts

/**
 * Wraps a node payload in the deep `$state` the real graph holds it in.
 *
 * `renderNode` hands a component a plain object, which is enough for the six
 * legacy node components: Svelte 4 semantics make `data.time = 2500` inside one
 * of those an invalidation of `data` itself, so the node re-renders whether or
 * not anything outside it is reactive.
 *
 * A runes component gets no such help. There, a write through `data` is only
 * seen if `data` is reactive - which in the app it always is, because
 * `graph.nodes` in `$lib/stores/flow.svelte` is deliberately *deep* `$state`
 * (see the note there; it is the reason in-place payload edits work at all).
 * A plain object in a test is therefore not a simplification of the real thing
 * but a different thing, and a runes node driven through one renders its
 * initial payload and then never moves.
 *
 * So this is not a test convenience: it is the harness catching up with the
 * contract. Reach for it whenever a runes node component is asked to re-render
 * from an edit to its own payload.
 *
 * The infix in the filename is load-bearing - runes only exist in `.svelte.ts`.
 */
export function asGraphState<T extends object>(data: T): T {
	const reactive = $state(data);
	return reactive;
}
