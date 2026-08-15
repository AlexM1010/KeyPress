<!-- KeyPressNode.svelte -->
<script lang="ts">
	import { Position } from '@xyflow/svelte';
	import { Keyboard } from 'lucide-svelte';
	import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
	import Checkbox from './nodeComponents/Checkbox.svelte';
	import Select from './nodeComponents/Select.svelte';
	import TimeInput from './nodeComponents/TimeInput.svelte';
	import ButtonGroup from './nodeComponents/ButtonGroup.svelte';
	import ButtonGroupItem from './nodeComponents/ButtonGroupItem.svelte';
	import type { ComponentType } from 'svelte';
	import type { HandleConfig } from './types';

	/**
	 * Data payload of a `KeyPressNode`.
	 *
	 * The node stores what the user *meant*, not the keystroke that expresses
	 * it, because the two are not the same thing on every keyboard. `!` is
	 * Shift+1 on a UK and a US keyboard but an unshifted key on a French one,
	 * and `@` is Shift+2 on a US keyboard and Shift+apostrophe on a UK one - so
	 * any keystroke written into the save file would be wrong on somebody's
	 * machine. `character` therefore holds the character itself, and the Go
	 * handler works out the keystroke at run time against whatever keyboard
	 * layout is in force at that moment. The flow is portable by construction:
	 * nothing to detect, nothing stored that can go stale, and switching layout
	 * mid-session needs no re-save.
	 *
	 * `namedKey` is the other half: keys that print no character at all (Enter,
	 * F5, the arrows) are the same key on every layout, so those are stored by
	 * name and need no resolution. `keyKind` says which of the two is in use.
	 * The UI calls that kind "Special"; the stored value stays `Named` because
	 * the Go handler and every saved flow already speak that word.
	 *
	 * `modifiers` is a set - the empty array means no modifier, so there is no
	 * "None" sentinel. Whatever the layout itself needs to reach the character
	 * (the Shift in Shift+1 for `!`) is worked out at run time and merged with
	 * this set, so the two can never fight.
	 *
	 * `pressDuration` is in **milliseconds** and only applies to the `Press`
	 * action, which holds the key down for that long before releasing it. `Hold`
	 * and `Release` are instantaneous toggles, so the field is ignored for them.
	 *
	 * Declared here rather than in `$lib/stores/flow` to keep the payload next
	 * to the only component that owns it, as `ColorPickerNode` already does.
	 */
	type KeyPressNodeData = {
		keyKind: 'Character' | 'Named';
		character: string;
		namedKey: string;
		keyAction: 'Press' | 'Hold' | 'Release';
		modifiers: string[];
		pressDuration: number;
	};

	// Props for the KeyPressNode
	export let id: string;
	export let title: string = 'Keypress';
	export let icon: ComponentType = Keyboard;
	export let color: string = 'bg-gradient-to-r from-orange-500 to-orange-600';
	export let highlightColor: string = 'bg-orange-500';

	/**
	 * The keys that print no character of their own.
	 *
	 * These are robotgo key names *verbatim* - the string the user picks is the
	 * string `actions_keyboard.go` hands to robotgo. A prettier display name
	 * would need a translation table on one side or the other, and any drift in
	 * that table would show up only as a keystroke that silently does nothing.
	 *
	 * Every name here is more than one character long, and that is a rule rather
	 * than a coincidence: robotgo reads a one-character name as a character and
	 * applies its own shift and US-layout rewrites to it, which is exactly what
	 * the Character key type exists to avoid. Characters never appear in this
	 * list.
	 *
	 * The keypad digits and its symbol keys (`num0`-`num9`, `num.`, `num+` and
	 * friends) are deliberately absent: every one of them prints a character, so
	 * the Character kind already covers what a user actually wants from them and
	 * covers it on any layout. `num_lock` stays because it prints nothing.
	 */
	export let namedKeys: string[] = [
		// Editing and navigation
		'enter',
		'tab',
		'space',
		'backspace',
		'delete',
		'insert',
		'escape',
		'home',
		'end',
		'pageup',
		'pagedown',
		'up',
		'down',
		'left',
		'right',
		'capslock',
		'printscreen',
		'menu',
		// Function row
		'f1',
		'f2',
		'f3',
		'f4',
		'f5',
		'f6',
		'f7',
		'f8',
		'f9',
		'f10',
		'f11',
		'f12',
		// The two keypad keys that print no character of their own
		'num_enter',
		'num_lock'
	];

	/**
	 * The list as the dropdown shows it: alphabetical, so a key is found by
	 * looking rather than by scanning the whole list.
	 *
	 * Sorted here rather than in the literal above so the grouping comments stay
	 * meaningful and a caller passing its own `namedKeys` gets the same ordering
	 * for free. `localeCompare` keeps `f2` before `f10` from being the only
	 * question this raises - it does not, so plain lexicographic order it is, and
	 * `f10` sitting between `f1` and `f2` is what someone reading an A-Z list
	 * expects anyway.
	 */
	$: sortedNamedKeys = [...namedKeys].sort((a, b) => a.localeCompare(b));

	/**
	 * The dropdown's options: the list above, plus whatever this node already
	 * holds if that is no longer on it.
	 *
	 * A flow saved before the keypad keys were dropped still names one, and the
	 * backend still runs it. Without this the `<select>` would find no matching
	 * option and render blank, so the node would show nothing where it should
	 * show `num5` - the choice would look lost while still being what runs.
	 */
	$: namedKeyOptions = sortedNamedKeys.includes(data.namedKey)
		? sortedNamedKeys
		: [...sortedNamedKeys, data.namedKey].sort((a, b) => a.localeCompare(b));

	// Typed as the payload's own unions rather than plain strings, so the button
	// groups below can write a choice straight into `data` without a cast.
	export let keyKinds: KeyPressNodeData['keyKind'][] = ['Character', 'Named'];
	export let keyActions: KeyPressNodeData['keyAction'][] = ['Press', 'Hold', 'Release'];

	/**
	 * What each key kind is called on screen.
	 *
	 * "Special" is the label only. Changing the stored value to match would make
	 * every already-saved Keypress node unreadable to the Go handler, which
	 * accepts `character` and `named` and nothing else, so the rename stops at
	 * the button face.
	 */
	const KEY_KIND_LABELS: Record<KeyPressNodeData['keyKind'], string> = {
		Character: 'Character',
		Named: 'Special'
	};

	/**
	 * The modifiers that can be held alongside the keystroke.
	 *
	 * Any combination is allowed, which is what makes `Ctrl+Shift+A`
	 * expressible. "Windows" is the Command key on macOS.
	 */
	const MODIFIER_OPTIONS = ['Ctrl', 'Alt', 'Shift', 'Windows'] as const;
	type ModifierName = (typeof MODIFIER_OPTIONS)[number];

	/**
	 * Arrow step of the Press Duration input, in milliseconds.
	 *
	 * The field is a `TimeInput`, the same control the Delay node uses, so a
	 * duration is typed and its unit switched here exactly as it is there rather
	 * than dragged along a bar with a range someone had to pick. That also means
	 * no upper bound: `TimeInput` reads `maxValue` in the *displayed* unit, so
	 * one that held at 1000 ms would read as 1000 seconds the moment the unit
	 * button was pressed.
	 *
	 * Worth knowing rather than enforcing: this is the *hold* time of a single
	 * keystroke, and key auto-repeat starts around half a second in on every
	 * desktop OS, so a long value stops being one keypress. A pause between
	 * keystrokes belongs in a Delay node.
	 */
	const PRESS_DURATION_STEP_MS = 10;

	const DEFAULT_DATA: KeyPressNodeData = {
		keyKind: 'Character',
		character: 'a',
		namedKey: 'enter',
		keyAction: 'Press',
		modifiers: [],
		pressDuration: 50
	};

	// Svelte Flow passes the node's data payload here, not the whole node - and it
	// is the *same object* the graph holds, so every edit below lands in the graph
	// by reference and is what gets saved. The controls therefore bind straight to
	// `data`; binding them to local props would drop the user's choices on save.
	//
	// That reference is the whole of the contract now. The graph's nodes are deep
	// `$state` (see `$lib/stores/flow.svelte`), so `data.modifiers = [...]` - or a
	// write to any other property, from a binding or from `syncModifiers` - is an
	// ordinary tracked write, and the edit *is* its own notification. Nothing has
	// to be announced afterwards, and no statement in this file has to come last
	// for an edit to be seen.
	export let data: KeyPressNodeData = { ...DEFAULT_DATA };

	// Backfill whatever a persisted node is missing without clobbering saved
	// values. This has to mutate `data` in place: reassigning it
	// (`data = { ...DEFAULT_DATA, ...data }`) would detach this component from
	// the store's object and silently discard every later edit.
	Object.assign(data, { ...DEFAULT_DATA, ...data });

	// `modifiers` is the one field a hand-edited save could hold a non-array in,
	// and every read below assumes an array. Same in-place rule as above.
	if (!Array.isArray(data.modifiers)) {
		data.modifiers = [...DEFAULT_DATA.modifiers];
	}

	// Node connection point configuration. Without these the node renders but
	// has nowhere for an edge to attach, so it could never be part of a flow.
	const handles: HandleConfig[] = [
		{ id: 'right', type: 'source', position: Position.Right, offsetY: 50 },
		{ id: 'left', type: 'target', position: Position.Left, offsetY: 50 }
	];

	// One checkbox per modifier, seeded from the payload. `Checkbox` exposes a
	// boolean, so the set in `data.modifiers` is mirrored here and written back
	// by `syncModifiers` rather than bound to directly.
	const modifierChecked: Record<ModifierName, boolean> = {
		Ctrl: false,
		Alt: false,
		Shift: false,
		Windows: false
	};
	for (const name of MODIFIER_OPTIONS) {
		modifierChecked[name] = data.modifiers.includes(name);
	}

	/**
	 * Push the checkbox state back into the payload, in a fixed order so the
	 * saved array does not churn as boxes are ticked and unticked.
	 *
	 * The equality check is what keeps this from writing on every reactive pass;
	 * the write itself only ever touches a property of `data`, never `data`
	 * itself, so the by-reference link to the graph survives.
	 */
	function syncModifiers(checked: Record<ModifierName, boolean>): void {
		const next = MODIFIER_OPTIONS.filter((name) => checked[name]);
		const current = data.modifiers;
		if (current.length === next.length && next.every((name, index) => current[index] === name)) {
			return;
		}
		data.modifiers = [...next];
	}

	// Depends on `modifierChecked` alone, so writing `data.modifiers` inside
	// cannot re-trigger it.
	$: syncModifiers(modifierChecked);

	/**
	 * Select the character box's contents, so the next key typed replaces what
	 * is there instead of being rejected.
	 *
	 * The box holds one character and `maxlength` is 1, so a click that leaves
	 * the caret *after* that character makes the field look editable while
	 * swallowing every keystroke - the only way forward would be Backspace
	 * first. Selecting on the way in makes the existing character the thing
	 * being overwritten, which is what clicking a one-character field is for.
	 *
	 * Wired to both focus and mouse-up because neither alone is enough: focus
	 * does not fire when an already-focused box is clicked again, and the
	 * browser collapses the selection to a caret on mouse-up, so that event is
	 * also where the default has to be suppressed. Dragging across a single
	 * character has nothing to offer that this takes away.
	 */
	function selectCharacter(event: Event): void {
		(event.currentTarget as HTMLInputElement).select();
	}

	$$restProps;
</script>

<NodeWrapper {icon} {title} {color} {id} type="KeyPressNode" {handles}>
	<!-- Node content for configuring the keystroke. The section headings and
         button groups are the same shapes the Mouse Move and Mouse Click nodes
         use, so this reads as one of the family rather than a form of its own.

         `nodrag` keeps Svelte Flow from turning a click on a control into a node
         drag; it is matched against the event target's ancestors, so wrapping
         the controls here covers all of them. -->
	<div class="nodrag grid gap-6">
		<!-- Which kind of key, and then the key itself -->
		<div class="grid gap-4">
			<h3 class="text-sm font-medium --main-text">Key</h3>
			<ButtonGroup variant="default">
				{#each keyKinds as kind (kind)}
					<ButtonGroupItem
						value={kind}
						on:click={() => (data.keyKind = kind)}
						active={data.keyKind === kind}
						itemHighlightColor={highlightColor}
					>
						{KEY_KIND_LABELS[kind]}
					</ButtonGroupItem>
				{/each}
			</ButtonGroup>

			{#if data.keyKind === 'Named'}
				<Select
					label="Special Key"
					options={namedKeyOptions}
					icon={Keyboard}
					bind:value={data.namedKey}
				/>
			{:else}
				<!-- A free-text box rather than a list: any character can be
                     typed, including ones no fixed list would think to offer.
                     One character only - this node is one keystroke. -->
				<div class="flex items-center gap-2">
					<label class="text-sm --main-text" for="character-{id}">Character</label>
					<input
						id="character-{id}"
						type="text"
						maxlength="1"
						autocomplete="off"
						spellcheck="false"
						placeholder="!"
						bind:value={data.character}
						on:focus={selectCharacter}
						on:mouseup|preventDefault={selectCharacter}
						class="character-input h-8 w-12 rounded-md px-2 text-center font-mono text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
					/>
				</div>
			{/if}
		</div>

		<!-- What to do with it, and for how long -->
		<div class="grid gap-4">
			<h3 class="text-sm font-medium --main-text">Key Action</h3>
			<ButtonGroup variant="default">
				{#each keyActions as action (action)}
					<ButtonGroupItem
						value={action}
						on:click={() => (data.keyAction = action)}
						active={data.keyAction === action}
						itemHighlightColor={highlightColor}
					>
						{action}
					</ButtonGroupItem>
				{/each}
			</ButtonGroup>

			<!-- Only meaningful for "Press": "Hold" and "Release" are
                 instantaneous toggles that leave the key down for later nodes to
                 deal with. -->
			{#if data.keyAction === 'Press'}
				<TimeInput
					label="Press Duration"
					bind:value={data.pressDuration}
					defaultValue={DEFAULT_DATA.pressDuration}
					startingUnit="ms"
					minValue={0}
					step={PRESS_DURATION_STEP_MS}
					{highlightColor}
				/>
			{/if}
		</div>

		<!-- Any combination, so Ctrl+Shift+A is one node rather than an
             impossible one. Nothing needs to be ticked; no ticks means a plain
             keystroke. -->
		<div class="grid gap-4">
			<h3 class="text-sm font-medium --main-text">Modifiers</h3>
			<div class="flex flex-wrap gap-x-4 gap-y-2">
				{#each MODIFIER_OPTIONS as name (name)}
					<Checkbox label={name} {highlightColor} bind:checked={modifierChecked[name]} />
				{/each}
			</div>
		</div>
	</div>
</NodeWrapper>

<style>
	/* The same surface the number and time inputs of the other nodes sit on, so
       the one text box this node has does not read as a control from a different
       app. */
	.character-input {
		background-color: var(--main);
		color: var(--main-text);
		transition: background-color 0.3s;
	}

	.character-input:hover {
		background-color: var(--main-hover);
	}
</style>
