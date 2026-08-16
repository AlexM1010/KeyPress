<!-- BranchNode.svelte -->
<script lang="ts">
	/**
	 * The node that asks a question and picks an output.
	 *
	 * A run carries a scratchpad of name -> value (`runState` in
	 * `backend/interpreter.go`), written by the "Store result as" field on the
	 * nodes that produce a result and read here. The condition is one comparison -
	 * a variable, an operator and a value - and never an expression: the whole
	 * condition language is the six operators below, which is what lets this be a
	 * dropdown rather than a parser. See `conditionHolds` in
	 * `backend/actions_branch.go`, which is the other half of every rule this file
	 * states.
	 *
	 * Written in runes, like `SequenceNode`. New files go on the runes side of the
	 * migration; a file is wholly one dialect or the other.
	 */
	import { Position } from '@xyflow/svelte';
	import { GitBranch } from 'lucide-svelte';
	import { untrack, type ComponentType } from 'svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import type { HandleConfig } from './types';

	/**
	 * The operators, spelled the way the payload spells them.
	 *
	 * These strings are the save format and the Go `switch` at once, so they are
	 * the option *values*; the words beside them are only what the dropdown shows.
	 * An operator outside this set is a payload the backend refuses outright
	 * rather than guessing a branch for - which is why the node offers a list and
	 * not a text box.
	 */
	type BranchOperator = 'equals' | 'notEquals' | 'greaterThan' | 'lessThan' | 'contains' | 'isSet';

	/** Data payload of a `BranchNode`: one comparison. */
	type BranchNodeData = {
		variable: string;
		operator: BranchOperator;
		value: string;
	};

	type Props = {
		id: string;
		title?: string;
		icon?: ComponentType;
		color?: string;
		isConnectable?: boolean;
		data?: BranchNodeData;
	};

	/**
	 * The six operators and how they read on screen.
	 *
	 * The labels are phrased so the three controls read as one sentence down the
	 * card - `spot.x` / `is greater than` / `800` - because that sentence is the
	 * only description of the condition the user ever sees.
	 */
	const OPERATORS: { id: BranchOperator; label: string }[] = [
		{ id: 'equals', label: 'equals' },
		{ id: 'notEquals', label: 'does not equal' },
		{ id: 'greaterThan', label: 'is greater than' },
		{ id: 'lessThan', label: 'is less than' },
		{ id: 'contains', label: 'contains' },
		{ id: 'isSet', label: 'is set' }
	];

	/**
	 * The one operator that ignores `value` entirely.
	 *
	 * `isSet` asks whether anything ever wrote to the name, so there is nothing to
	 * compare against; the backend does not read `value` at all on that path. The
	 * field is therefore taken off the card rather than left there doing nothing -
	 * an input whose contents change no outcome is a lie about what the node does.
	 * What was typed into it stays in the payload, so switching operator away and
	 * back does not cost the user their value.
	 */
	const VALUELESS_OPERATOR: BranchOperator = 'isSet';

	/**
	 * The output handles, which are the words `true` and `false`.
	 *
	 * The handler returns one of these two strings and the walk takes the single
	 * edge drawn from that handle (`branchTrue` / `branchFalse` in
	 * `actions_branch.go`), so the ids here are the save format: an edge's
	 * `sourceHandle` is one of them, and renaming either would strand every macro
	 * that had drawn one.
	 */
	const TRUE_HANDLE = 'true';
	const FALSE_HANDLE = 'false';

	const DEFAULT_DATA: BranchNodeData = {
		variable: '',
		operator: 'equals',
		value: ''
	};

	let {
		id,
		title = 'Branch',
		icon = GitBranch,
		color = 'bg-gradient-to-r from-purple-500 to-purple-600',
		isConnectable = true,
		data = { ...DEFAULT_DATA }
	}: Props = $props();

	// Svelte Flow hands over the very `data` object the graph holds, so the edits
	// below land in the graph by reference and are what gets saved. Backfilled in
	// place for that reason - reassigning `data` would detach this component from
	// the store's object and silently discard every later edit - and inside
	// `untrack` because this file is runes: reading a reactive prop bare at the
	// top level warns, and a backfill that re-ran would put the defaults back over
	// the user's saved condition. See `SequenceNode`, which does the same.
	untrack(() => Object.assign(data, { ...DEFAULT_DATA, ...data }));

	/** Whether the value field is part of this condition at all. */
	const takesAValue = $derived(data.operator !== VALUELESS_OPERATOR);

	/**
	 * One way in and two ways out, the true one above the false one.
	 *
	 * The vertical order is not decoration: the two edges leaving this node are
	 * told apart on the canvas by where they leave from, and a user who has to
	 * click a node to find out which wire is which has been given a coin toss. The
	 * legend in the card repeats the same order.
	 */
	const handles: HandleConfig[] = [
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 },
		{ id: TRUE_HANDLE, type: 'source', position: Position.Right, offsetY: 35 },
		{ id: FALSE_HANDLE, type: 'source', position: Position.Right, offsetY: 65 }
	];
</script>

<NodeWrapper {id} {icon} {title} {color} type="Branch" {handles} {isConnectable}>
	<!-- `nodrag` keeps a click on a control from becoming a node drag; it is
	     matched against the event target's ancestors, so one wrapper covers the
	     lot. -->
	<div class="nodrag grid gap-4">
		<!-- The name the condition reads out of the run state. -->
		<div class="grid gap-1.5">
			<label class="branch-label" for="branch-variable-{id}">Variable</label>
			<input
				id="branch-variable-{id}"
				type="text"
				autocomplete="off"
				spellcheck="false"
				placeholder="matched"
				bind:value={data.variable}
				class="branch-input"
			/>
			<p class="branch-help">
				A name an earlier node stored its result as. A Mouse Move storing
				<code>spot</code> writes <code>spot.x</code> and <code>spot.y</code>, so the name to read
				here is one of those.
			</p>
		</div>

		<!-- The comparison. A fixed list, because it is the whole language. -->
		<div class="grid gap-1.5">
			<label class="branch-label" for="branch-operator-{id}">Comparison</label>
			<select id="branch-operator-{id}" bind:value={data.operator} class="branch-input">
				{#each OPERATORS as operator (operator.id)}
					<option value={operator.id}>{operator.label}</option>
				{/each}
			</select>
		</div>

		{#if takesAValue}
			<!-- What to compare against. Kept as a string: the backend parses a
			     numeric one back into a number, so a typed 800 compares numerically
			     against a stored 800 and a typed word compares as text. -->
			<div class="grid gap-1.5">
				<label class="branch-label" for="branch-value-{id}">Value</label>
				<input
					id="branch-value-{id}"
					type="text"
					autocomplete="off"
					spellcheck="false"
					placeholder="00ff00"
					bind:value={data.value}
					class="branch-input"
				/>
			</div>
		{:else}
			<p class="branch-help">
				<code>is set</code> asks only whether anything wrote to the name, so it compares against nothing.
			</p>
		{/if}

		<!-- Which wire is which. The rows are in the same order as the handles
		     down the right-hand side. -->
		<ul class="branch-outputs">
			<li class="branch-output">
				<span class="branch-output-name branch-true">{TRUE_HANDLE}</span>
				<span class="branch-output-when">the condition holds</span>
			</li>
			<li class="branch-output">
				<span class="branch-output-name branch-false">{FALSE_HANDLE}</span>
				<span class="branch-output-when">it does not, or the variable is unset</span>
			</li>
		</ul>
	</div>
</NodeWrapper>

<style>
	.branch-label {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--main-text);
	}

	/* Themed like the shared number and select inputs rather than left to the
	   browser's default, since a node sits on a translucent card that is dark
	   under the dark theme. */
	.branch-input {
		width: 100%;
		height: 2rem;
		padding: 0 0.5rem;
		border-radius: 0.375rem;
		border: 1px solid var(--border);
		background-color: var(--main);
		color: var(--main-text);
		font-size: 0.8rem;
		transition: background-color 0.3s;
	}

	.branch-input:hover {
		background-color: var(--main-hover);
	}

	.branch-input:focus {
		outline: none;
	}

	/* The dropdown list is painted by the browser, which does not inherit the
	   control's colours into it on every platform. */
	.branch-input option {
		background-color: var(--secondary);
		color: var(--main-text);
	}

	.branch-help {
		font-size: 0.7rem;
		line-height: 1.1rem;
		color: var(--main-text);
		opacity: 0.75;
	}

	.branch-help code {
		font-family: monospace;
	}

	.branch-outputs {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.branch-output {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		background: var(--main);
	}

	.branch-output-name {
		font-family: monospace;
		font-size: 0.7rem;
		font-weight: 600;
		padding: 0.1rem 0.4rem;
		border-radius: 0.25rem;
		color: var(--secondary-text);
	}

	.branch-true {
		background: rgba(22, 163, 74, 0.9);
	}

	.branch-false {
		background: rgba(220, 38, 38, 0.9);
	}

	.branch-output-when {
		font-size: 0.7rem;
		opacity: 0.75;
	}
</style>
