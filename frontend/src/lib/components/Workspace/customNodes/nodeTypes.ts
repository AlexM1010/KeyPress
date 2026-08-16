// src/lib/nodeTypes.ts
import type { NodeTypes } from '@xyflow/svelte';
import ColorPickerNode from './ColorPickerNode.svelte';
import MouseClickNode from './MouseClickNode.svelte';
import KeyPressNode from './KeyPressNode.svelte';
import StartNode from './StartNode.svelte';
import MouseMoveNode from './MouseMoveNode.svelte';
import DelayNode from './DelayNode.svelte';
import SequenceNode from './SequenceNode.svelte';
import BranchNode from './BranchNode.svelte';
import LoopStartNode from './LoopStartNode.svelte';
import LoopEndNode from './LoopEndNode.svelte';

/**
 * The node `type` string a saved node carries -> the component that draws it.
 *
 * Every key here is part of the save format: it is written into each node's
 * `type` and read back by `Flow.svelte`, so renaming one silently breaks every
 * macro on disk that used it. `nodeLabels.ts` maps the same strings to the
 * names the user sees.
 *
 * Each entry used to be cast `as unknown as typeof SvelteComponent`. Svelte
 * Flow 0.1.x typed `NodeTypes` as a map of Svelte 4 component *classes*, and a
 * component compiled by Svelte 5 is a function, so the two shapes could only be
 * reconciled by lying to the compiler. 1.x types the map as
 * `Record<string, Component<NodeProps & ...>>` - the Svelte 5 component type -
 * which is what these components already are, so the lie is no longer needed
 * and the entries are the plain imports.
 */
export const nodeTypes: NodeTypes = {
	ColorPickerNode: ColorPickerNode,
	MouseClickNode: MouseClickNode,
	KeyPressNode: KeyPressNode,
	StartNode: StartNode,
	MouseMoveNode: MouseMoveNode,
	DelayNode: DelayNode,
	SequenceNode: SequenceNode,
	BranchNode: BranchNode,
	// The two halves of a loop. Both are registered, although the palette offers
	// only one entry ("Loop") that drops the pair: a saved macro's Loop End needs
	// a component to be drawn with, and the pair is created by `createLoopPair`
	// rather than by dropping each half.
	LoopStartNode: LoopStartNode,
	LoopEndNode: LoopEndNode
};
