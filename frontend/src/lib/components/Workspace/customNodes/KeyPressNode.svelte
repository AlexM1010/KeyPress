<!-- KeyPressNode.svelte -->
<script lang="ts">
    import { Position } from '@xyflow/svelte';
    import { Keyboard } from 'lucide-svelte';
    import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
    import Checkbox from './nodeComponents/Checkbox.svelte';
    import Select from './nodeComponents/Select.svelte';
    import Slider from './nodeComponents/Slider.svelte';
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
     */
    export let namedKeys: string[] = [
        // Editing and navigation
        'enter', 'tab', 'space', 'backspace', 'delete', 'insert', 'escape',
        'home', 'end', 'pageup', 'pagedown',
        'up', 'down', 'left', 'right',
        'capslock', 'printscreen', 'menu',
        // Function row
        'f1', 'f2', 'f3', 'f4', 'f5', 'f6',
        'f7', 'f8', 'f9', 'f10', 'f11', 'f12',
        // Numeric keypad, which is its own set of keys rather than the digit row
        'num0', 'num1', 'num2', 'num3', 'num4',
        'num5', 'num6', 'num7', 'num8', 'num9',
        'num.', 'num+', 'num-', 'num*', 'num/', 'num_enter', 'num_lock'
    ];

    export let keyKinds: string[] = ['Character', 'Named'];
    export let keyActions: string[] = ['Press', 'Hold', 'Release'];

    /**
     * The modifiers that can be held alongside the keystroke.
     *
     * Any combination is allowed, which is what makes `Ctrl+Shift+A`
     * expressible. "Windows" is the Command key on macOS.
     */
    const MODIFIER_OPTIONS = ['Ctrl', 'Alt', 'Shift', 'Windows'] as const;
    type ModifierName = (typeof MODIFIER_OPTIONS)[number];

    /**
     * Bounds of the Press Duration slider, in milliseconds.
     *
     * `Slider` defaults to a 0-100 range labelled `%`, which is wrong twice over
     * for this field: it reads as a percentage, and because the slider clamps
     * and writes back through its binding it would quietly rewrite a saved value
     * above 100 down to 100. Passing millisecond-appropriate bounds fixes both
     * without touching the shared component's defaults, which `MouseMoveNode`'s
     * percentage slider still relies on.
     *
     * One second is the ceiling because this is the *hold* time of a single
     * keystroke, not a wait: key auto-repeat starts around half a second in on
     * every desktop OS, so past that it stops being one keypress. A pause
     * between keystrokes belongs in a Delay node, which has a real time input.
     */
    const PRESS_DURATION_MIN_MS = 0;
    const PRESS_DURATION_MAX_MS = 1000;
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
    // is the *same object* the flow store holds, so every edit below lands in the
    // store by reference and is picked up by `toObject()` on save. The controls
    // therefore bind straight to `data`; binding them to local props would drop
    // the user's choices on save.
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
     * itself, so the by-reference link to the flow store survives.
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

    // These read `data.*`, and Svelte 4 invalidates `data` on every `bind:`
    // write to one of its properties, so the preview tracks the controls. Note
    // they only *read* - nothing here reassigns `data`, which would detach this
    // component from the flow store.
    $: chosenKey = data.keyKind === 'Named' ? data.namedKey : data.character;

    // Filtered so an empty Character box shows no chip at all rather than an
    // empty one that reads as a key.
    $: comboParts = [...data.modifiers, chosenKey].filter((part) => part !== '');

    $: actionSummary =
        data.keyAction === 'Hold'
            ? 'Pushed down and left down until a Release node lets it up.'
            : data.keyAction === 'Release'
              ? 'Let back up.'
              : `Held down for ${data.pressDuration} ms, then released.`;

    /**
     * What the keystroke types, or '' when claiming anything would be a guess.
     *
     * A character node can promise the character itself, because the keystroke
     * is worked out from the live keyboard layout rather than baked into the
     * save - that promise is the whole point of storing the character. It cannot
     * promise anything once a modifier is added on top, because Shift over `1`
     * is `!` on a UK keyboard and something else elsewhere, and Ctrl or Alt make
     * it a shortcut rather than text at all. A named key types nothing by
     * definition, and so does Release.
     */
    $: typedSummary =
        data.keyAction === 'Release' || data.keyKind === 'Named'
            ? ''
            : data.character === ''
              ? 'Pick a character for this node to type.'
              : data.modifiers.length > 0
                ? `Sends the combination above. What that types depends on the keyboard, so drop the modifiers if you just want to type “${data.character}”.`
                : `Types “${data.character}”, whatever keyboard layout is in use when the flow runs.`;

    $$restProps;
</script>

<NodeWrapper
    {icon}
    {title}
    {color}
    {id}
    type="KeyPressNode"
    {handles}
    on:duplicate
    on:delete
>
    <!-- Node content for configuring the keystroke.

         `nodrag` keeps Svelte Flow from turning a click on a control into a node
         drag; it is matched against the event target's ancestors, so wrapping
         the controls here covers all of them. -->
    <div class="nodrag space-y-4">
        <div class="space-y-1.5">
            <Select label="Key Type" options={keyKinds} bind:value={data.keyKind} />
            <p class="text-xs opacity-70">
                <b class="font-medium">Character</b> types a character -
                <b class="font-mono">a</b>, <b class="font-mono">A</b>,
                <b class="font-mono">!</b>, <b class="font-mono">£</b> - and finds the key for it
                on whatever keyboard is in use. <b class="font-medium">Named</b> presses a key with
                no character of its own.
            </p>
        </div>

        {#if data.keyKind === 'Named'}
            <Select label="Key" options={namedKeys} icon={Keyboard} bind:value={data.namedKey} />
        {:else}
            <!-- A free-text box rather than a list: any character can be typed,
                 including ones no fixed list would think to offer. One character
                 only - this node is one keystroke. -->
            <div class="space-y-1.5">
                <label class="block text-xs font-medium --main-text" for="character-{id}">
                    Character
                </label>
                <input
                    id="character-{id}"
                    type="text"
                    maxlength="1"
                    autocomplete="off"
                    spellcheck="false"
                    placeholder="!"
                    bind:value={data.character}
                    class="w-full rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-center font-mono text-sm focus:border-transparent focus:ring-2 focus:ring-blue-400"
                />
            </div>
        {/if}

        <Select label="Key Action" options={keyActions} bind:value={data.keyAction} />

        <!-- Any combination, so Ctrl+Shift+A is one node rather than an
             impossible one. Nothing needs to be ticked; no ticks means a plain
             keystroke. -->
        <div class="space-y-1.5">
            <span class="block text-xs font-medium --main-text">Modifiers</span>
            <div class="flex flex-wrap gap-x-4 gap-y-2">
                {#each MODIFIER_OPTIONS as name (name)}
                    <Checkbox label={name} {highlightColor} bind:checked={modifierChecked[name]} />
                {/each}
            </div>
        </div>

        <!-- Only meaningful for "Press": "Hold" and "Release" are instantaneous
             toggles that leave the key down for later nodes to deal with. -->
        {#if data.keyAction === 'Press'}
            <Slider
                label="Press Duration"
                unit="ms"
                min={PRESS_DURATION_MIN_MS}
                max={PRESS_DURATION_MAX_MS}
                step={PRESS_DURATION_STEP_MS}
                defaultValue={DEFAULT_DATA.pressDuration}
                bind:value={data.pressDuration}
            />
        {/if}

        <!-- Effective keystroke, so the combination the controls add up to is
             readable at a glance instead of being assembled in the user's head.
             The chips repeat the chosen values verbatim rather than prettifying
             them, so there is no display name that could drift from what the
             backend is actually sent. -->
        <!-- The text colour is pinned rather than inherited. The labels above sit
             on the node's own translucent background and inherit the theme's text
             colour, which is white under the dark theme; this panel has an opaque
             light background of its own (the same `bg-gray-50` the Selects use),
             so inherited white would be invisible on it. -->
        <div
            class="space-y-1.5 rounded-lg border border-gray-200 bg-gray-50 px-3 py-2 text-gray-700"
        >
            <span class="block text-xs font-medium">Keystroke</span>
            <div class="flex flex-wrap items-center gap-1">
                {#each comboParts as part, index (index)}
                    {#if index > 0}
                        <span class="text-xs opacity-60">+</span>
                    {/if}
                    <kbd
                        class="rounded border border-gray-300 bg-white px-1.5 py-0.5 font-mono text-xs text-gray-800 shadow-sm"
                        >{part}</kbd
                    >
                {/each}
            </div>
            <p class="text-xs opacity-70">
                {actionSummary}{typedSummary ? ` ${typedSummary}` : ''}
            </p>
        </div>
    </div>
</NodeWrapper>
