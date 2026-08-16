<!-- LoopStartNode.svelte -->
<script lang="ts">
	/**
	 * The top of a loop: the node that decides whether to run the body again.
	 *
	 * A loop is a pair - this node and the `LoopEndNode` whose `back` edge returns
	 * here - and **the editor owns the edge between them**, not the user. Creating
	 * one of these creates the other (`createLoopPair` in `$lib/stores/flow.svelte`),
	 * deleting either deletes both, and duplicating either yields a whole new pair.
	 * `backend/actions_loop.go` has the argument for that; the short version is that
	 * a user-drawn back edge leaves "which loop does this belong to" with no answer.
	 *
	 * What this node holds is the loop's *reason to stop*, and there are two of
	 * them, both optional:
	 *
	 * - a **count**, how many times the body may run;
	 * - a **condition**, in the Branch node's shape, checked *before* each
	 *   iteration - so a condition that already holds runs the body zero times.
	 *
	 * All four combinations are legal, **including neither**: a loop with no count
	 * and no condition runs until the user presses the trigger hotkey again, and
	 * that is the canonical macro this app exists for rather than a mistake to be
	 * refused. The card says so in as many words, because a node that looks
	 * unconfigured and a node that means "forever" have to be told apart at a
	 * glance.
	 *
	 * Written in runes, like `SequenceNode` and `BranchNode`. New files go on the
	 * runes side of the migration; a file is wholly one dialect or the other.
	 */
	import { Position } from '@xyflow/svelte';
	import { Infinity as InfinityIcon, Repeat } from 'lucide-svelte';
	import { untrack, type ComponentType } from 'svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import type { HandleConfig } from './types';
	import { LOOP_BODY_HANDLE, LOOP_DONE_HANDLE } from '$lib/stores/flow.svelte';

	/**
	 * The operators, spelled the way the payload spells them.
	 *
	 * The same six the Branch node offers, and deliberately so: the loop's
	 * condition is evaluated by the very same `conditionHolds` in Go, and a macro
	 * should not have to learn two condition languages. An operator outside this
	 * set is a malformed payload the backend refuses - which is why this is a
	 * dropdown and not a text box.
	 */
	type LoopOperator = 'equals' | 'notEquals' | 'greaterThan' | 'lessThan' | 'contains' | 'isSet';

	/**
	 * Data payload of a `LoopStartNode`.
	 *
	 * Every field is optional, and the two "off" states are the point of the type:
	 *
	 * - `count` is `null` for **no count**. It cannot be `0`, because zero is a
	 *   real count - a body that runs no times - and the backend keeps `count` and
	 *   `hasCount` apart for exactly this reason (`loopConfig` in
	 *   `actions_loop.go`). `null` is what an emptied number input yields, and it
	 *   is one of the four spellings of "no count" Go accepts.
	 * - `operator` is **absent** for no condition. There is no `''` member in
	 *   `LoopOperator` and none is added: an empty operator is not a comparison the
	 *   node could make, so "no condition" is the absence of a choice rather than a
	 *   choice with an empty name. Go reads an empty *or* absent operator the same
	 *   way, so this is the stricter of two accepted spellings.
	 *
	 * `loopId` is the editor's pairing id, written by the store when the pair is
	 * created and never read here or in Go. It is declared so that this component
	 * cannot accidentally drop it while backfilling.
	 */
	type LoopStartNodeData = {
		count?: number | null;
		variable?: string;
		operator?: LoopOperator;
		value?: string;
		loopId?: string;
	};

	type Props = {
		id: string;
		title?: string;
		icon?: ComponentType;
		color?: string;
		isConnectable?: boolean;
		data?: LoopStartNodeData;
	};

	/** The six operators and how they read on screen, as in `BranchNode`. */
	const OPERATORS: { id: LoopOperator; label: string }[] = [
		{ id: 'equals', label: 'equals' },
		{ id: 'notEquals', label: 'does not equal' },
		{ id: 'greaterThan', label: 'is greater than' },
		{ id: 'lessThan', label: 'is less than' },
		{ id: 'contains', label: 'contains' },
		{ id: 'isSet', label: 'is set' }
	];

	/** The one operator that ignores `value`; see `BranchNode` for why the field
	 *  is taken off the card rather than left there doing nothing. */
	const VALUELESS_OPERATOR: LoopOperator = 'isSet';

	/**
	 * The value of the dropdown's first entry, which is not an operator.
	 *
	 * A `<select>` has to have *some* string for "none", and this is it - but it
	 * never reaches the payload: choosing it deletes `operator` instead of writing
	 * an empty one. See `chooseOperator`.
	 */
	const NO_OPERATOR = '';

	/**
	 * Defaults for a payload that has none. No `operator` key, which is the whole
	 * point: a loop dropped from the palette has no condition, and "no condition"
	 * is spelled by the key not being there.
	 */
	const DEFAULT_DATA: LoopStartNodeData = {
		count: null,
		variable: '',
		value: ''
	};

	let {
		id,
		title = 'Loop Start',
		icon = Repeat,
		color = 'bg-gradient-to-r from-teal-500 to-teal-600',
		isConnectable = true,
		data = { ...DEFAULT_DATA }
	}: Props = $props();

	// Backfilled in place and inside `untrack`, for the reasons spelled out in
	// `SequenceNode`: Svelte Flow hands over the very object the graph holds, so
	// reassigning `data` would detach this component from it and silently discard
	// every later edit, and a backfill that re-ran would put the defaults back over
	// the user's saved loop.
	untrack(() => Object.assign(data, { ...DEFAULT_DATA, ...data }));

	/** Whether the payload asks for a count at all - `0` does, `null` does not. */
	const hasCount = $derived(typeof data.count === 'number');

	/** Whether the payload asks for a condition: an operator that is there. */
	const hasCondition = $derived(data.operator !== undefined);

	/** Whether the condition's value field is part of this comparison. */
	const takesAValue = $derived(hasCondition && data.operator !== VALUELESS_OPERATOR);

	/** A loop with neither reason to stop, which runs until the user says stop. */
	const runsForever = $derived(!hasCount && !hasCondition);

	/** What the count box shows. `null` shows an empty box, and an empty box is
	 *  what the user clears the count with - the two have to be the same thing. */
	const countText = $derived(hasCount ? String(data.count) : '');

	/**
	 * Reads the count box.
	 *
	 * An explicit handler rather than `bind:value`, because the two things a
	 * binding cannot say are exactly the two that matter here: an emptied box has
	 * to write `null` rather than `0` or `NaN`, and a value the backend refuses
	 * outright - a negative, a fraction, an infinity - must never reach the payload
	 * at all. `parseLoopCount` rejects all three rather than rounding them, so a
	 * macro that could not be run is a macro this editor must not be able to draw.
	 */
	function readCount(event: Event & { currentTarget: HTMLInputElement }) {
		const text = event.currentTarget.value.trim();
		if (text === '') {
			data.count = null;
			return;
		}

		const parsed = Number(text);
		data.count = Number.isFinite(parsed) ? Math.max(0, Math.trunc(parsed)) : null;
	}

	/**
	 * Reads the comparison dropdown, and deletes the key for "no condition".
	 *
	 * `delete` rather than `data.operator = undefined`: the payload is serialised
	 * with `JSON.stringify`, which drops an undefined value anyway, so writing one
	 * would make the in-memory graph and the saved file disagree about the shape of
	 * the same node. Deleting says it once.
	 */
	function chooseOperator(event: Event & { currentTarget: HTMLSelectElement }) {
		const chosen = event.currentTarget.value;
		if (chosen === NO_OPERATOR) {
			delete data.operator;
			return;
		}
		data.operator = chosen as LoopOperator;
	}

	/** The chosen operator as the dropdown words it, for the summary line. */
	const operatorLabel = $derived(
		OPERATORS.find((entry) => entry.id === data.operator)?.label ?? ''
	);

	/** The condition as one readable clause: `spot.x is greater than 800`. */
	const conditionSentence = $derived(
		`${data.variable?.trim() || 'a variable'} ${operatorLabel}` +
			(takesAValue ? ` ${data.value?.trim() || '...'}` : '')
	);

	const countSentence = $derived(
		data.count === 1 ? 'Runs its body once' : `Runs its body ${data.count} times`
	);

	/**
	 * What this loop does, in one line, for the card's first row.
	 *
	 * Both reasons to stop are reported when both are set, and as "whichever comes
	 * first", because that is what the node really does - `executeLoopStartTask`
	 * checks the condition and then the count on every arrival, and either ends the
	 * loop. A count of zero says so explicitly: it is the one configuration whose
	 * effect (nothing) is easy to mistake for a mistake.
	 *
	 * Empty when the loop runs forever - that state has a badge of its own rather
	 * than a sentence, and the badge is what the markup renders instead.
	 */
	const summary = $derived.by(() => {
		if (runsForever) return '';
		if (hasCount && hasCondition) {
			return `${countSentence}, or until ${conditionSentence} - whichever comes first.`;
		}
		if (hasCount) {
			return data.count === 0 ? `${countSentence} - the body never runs.` : `${countSentence}.`;
		}
		return `Runs until ${conditionSentence}.`;
	});

	/**
	 * One way in and two ways out, `body` above `done`.
	 *
	 * The ids are the save format - `executeLoopStartTask` returns one of these two
	 * strings and the walk takes the single edge drawn from that handle - so they
	 * come from the store, which is also what draws the pair's `back` edge. The
	 * vertical order is the same argument the Branch node makes: two wires leaving
	 * one card have to be tellable apart on the canvas, and the legend inside the
	 * card repeats the order.
	 */
	const handles: HandleConfig[] = [
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 },
		{ id: LOOP_BODY_HANDLE, type: 'source', position: Position.Right, offsetY: 35 },
		{ id: LOOP_DONE_HANDLE, type: 'source', position: Position.Right, offsetY: 65 }
	];
</script>

<NodeWrapper {id} {icon} {title} {color} type="Loop Start" {handles} {isConnectable}>
	<!-- `nodrag` keeps a click on a control from becoming a node drag. -->
	<div class="nodrag grid gap-4">
		<!-- What the loop does, in one line, before any of the controls. A loop
		     with nothing set is the one reading that has to be unmistakable: it is
		     legal, it is common, and it looks exactly like a node nobody finished
		     filling in. -->
		{#if runsForever}
			<p class="loop-forever" data-testid="loop-summary">
				<InfinityIcon class="w-4 h-4" />
				<span>Loops forever - until the macro is stopped</span>
			</p>
		{:else}
			<p class="loop-summary" data-testid="loop-summary">{summary}</p>
		{/if}

		<!-- The count. Empty is not zero, and the help text below says so. -->
		<div class="grid gap-1.5">
			<label class="loop-label" for="loop-count-{id}">Repeat count</label>
			<input
				id="loop-count-{id}"
				type="number"
				min="0"
				step="1"
				autocomplete="off"
				placeholder="no limit"
				value={countText}
				oninput={readCount}
				class="loop-input"
			/>
			<p class="loop-help">
				How many times the body runs. Leave it empty for no limit; <code>0</code> is a count, and skips
				the body entirely.
			</p>
		</div>

		<!-- The condition, in the Branch node's shape and evaluated by the same Go
		     code. "No condition" is the first entry, and it is the default. -->
		<div class="grid gap-1.5">
			<label class="loop-label" for="loop-operator-{id}">Stop when</label>
			<select
				id="loop-operator-{id}"
				value={data.operator ?? NO_OPERATOR}
				onchange={chooseOperator}
				class="loop-input"
			>
				<option value={NO_OPERATOR}>nothing - no condition</option>
				{#each OPERATORS as operator (operator.id)}
					<option value={operator.id}>a variable {operator.label}</option>
				{/each}
			</select>
		</div>

		{#if hasCondition}
			<div class="grid gap-1.5">
				<label class="loop-label" for="loop-variable-{id}">Variable</label>
				<input
					id="loop-variable-{id}"
					type="text"
					autocomplete="off"
					spellcheck="false"
					placeholder="matched"
					bind:value={data.variable}
					class="loop-input"
				/>
				<p class="loop-help">
					A name an earlier node stored its result as. Checked <em>before</em> each iteration, so a condition
					that already holds runs the body no times at all.
				</p>
			</div>

			{#if takesAValue}
				<div class="grid gap-1.5">
					<label class="loop-label" for="loop-value-{id}">Value</label>
					<input
						id="loop-value-{id}"
						type="text"
						autocomplete="off"
						spellcheck="false"
						placeholder="00ff00"
						bind:value={data.value}
						class="loop-input"
					/>
				</div>
			{:else}
				<p class="loop-help">
					<code>is set</code> asks only whether anything wrote to the name, so it compares against nothing.
				</p>
			{/if}
		{/if}

		<!-- Which wire is which, in the order the handles sit down the side. -->
		<ul class="loop-outputs">
			<li class="loop-output">
				<span class="loop-output-name loop-body">{LOOP_BODY_HANDLE}</span>
				<span class="loop-output-when">one turn of the loop, ending at the Loop End</span>
			</li>
			<li class="loop-output">
				<span class="loop-output-name loop-done">{LOOP_DONE_HANDLE}</span>
				<span class="loop-output-when">taken once the loop is over</span>
			</li>
		</ul>
	</div>
</NodeWrapper>

<style>
	.loop-label {
		font-size: 0.75rem;
		font-weight: 500;
		color: var(--main-text);
	}

	/* Themed like the shared number and select inputs rather than left to the
	   browser's default, since a node sits on a translucent card that is dark
	   under the dark theme. */
	.loop-input {
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

	.loop-input:hover {
		background-color: var(--main-hover);
	}

	.loop-input:focus {
		outline: none;
	}

	/* The dropdown list is painted by the browser, which does not inherit the
	   control's colours into it on every platform. */
	.loop-input option {
		background-color: var(--secondary);
		color: var(--main-text);
	}

	.loop-help {
		font-size: 0.7rem;
		line-height: 1.1rem;
		color: var(--main-text);
		opacity: 0.75;
	}

	.loop-help code {
		font-family: monospace;
	}

	.loop-summary {
		font-size: 0.75rem;
		line-height: 1.1rem;
		color: var(--main-text);
	}

	/* Deliberately louder than the summary it replaces. "No count and no
	   condition" is the one state that reads as an unfinished node, so it is the
	   one state that says what it means in a badge rather than in prose. */
	.loop-forever {
		display: flex;
		align-items: center;
		gap: 0.4rem;
		padding: 0.3rem 0.5rem;
		border-radius: 0.375rem;
		background: rgba(13, 148, 136, 0.9);
		color: var(--secondary-text);
		font-size: 0.75rem;
		font-weight: 600;
	}

	.loop-outputs {
		display: flex;
		flex-direction: column;
		gap: 0.25rem;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	.loop-output {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.25rem 0.5rem;
		border-radius: 0.375rem;
		background: var(--main);
	}

	.loop-output-name {
		font-family: monospace;
		font-size: 0.7rem;
		font-weight: 600;
		padding: 0.1rem 0.4rem;
		border-radius: 0.25rem;
		color: var(--secondary-text);
	}

	.loop-body {
		background: rgba(13, 148, 136, 0.9);
	}

	.loop-done {
		background: rgba(71, 85, 105, 0.9);
	}

	.loop-output-when {
		font-size: 0.7rem;
		opacity: 0.75;
	}
</style>
