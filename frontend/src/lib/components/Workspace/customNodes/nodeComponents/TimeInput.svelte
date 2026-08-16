<!-- frontend\src\lib\components\customNodes\TimeInput.svelte -->
<script lang="ts">
	import { ChevronUp, ChevronDown } from 'lucide-svelte';
	import { untrack } from 'svelte';
	import '$lib/index.scss';

	type TimeUnit = 'ms' | 's' | 'min';

	interface ConversionFactors {
		ms: number;
		s: number;
		min: number;
	}

	const TO_MS: ConversionFactors = {
		ms: 1,
		s: 1000,
		min: 60 * 1000
	};

	// Arrow step per unit, in ms: 50ms at a time in ms, otherwise a whole unit.
	const STEP_MS: ConversionFactors = {
		ms: 50,
		s: 1000,
		min: 60 * 1000
	};

	const UNIT_DISPLAY_NAMES: Record<TimeUnit, string> = {
		ms: 'milliseconds',
		s: 'seconds',
		min: 'minutes'
	};

	type Props = {
		label?: string;
		id?: string;
		defaultValue?: number;
		value?: number;
		highlightColor?: string;
		startingUnit?: TimeUnit;
		showArrows?: boolean;
		/** Arrow step override, in milliseconds. Defaults to STEP_MS for the current unit. */
		step?: number | null;
		minValue?: number | null;
		maxValue?: number | null;
	};

	let {
		label = '',
		// The DOM id of the number box, tying it to its <label for=...>. It used to
		// be the hard-coded literal "time-input", so every instance on the page
		// rendered the same id: the Delay node's Random mode alone puts two of these
		// in one node, so the document had duplicate ids and *both* labels resolved
		// to the first box - clicking "Maximum Time" focused the minimum. A fresh
		// unique value per instance is the same fix Select.svelte and Slider.svelte
		// already carry; callers that want a stable, predictable id can still pass
		// one in.
		id = `time-input-${crypto.randomUUID()}`,
		// Destructured before `value`, because `value`'s fallback is written in
		// terms of it - exactly as the two `export let`s were ordered before.
		defaultValue = 1,
		// `$bindable` because every caller binds it to a field of the node's `data`
		// payload, and the box writes back: typing, stepping and switching unit all
		// assign `value`, always in milliseconds whatever the box is showing.
		value = $bindable(defaultValue * 1000),
		highlightColor = 'bg-grey-500',
		startingUnit = 's',
		showArrows = true,
		step = null,
		minValue = null,
		maxValue = null
	}: Props = $props();

	// Seeded from the prop and thereafter owned by this component - the unit
	// button changes it and nothing outside can. Reading the prop once is what
	// `let unit = startingUnit` did too: a later change to `startingUnit` did not
	// move a box the user had already switched, and still does not.
	//
	// `untrack` is how that gets said out loud. Reading a prop straight into
	// `$state` is the shape of a common mistake - someone expecting the state to
	// follow the prop - so the compiler warns on it (`state_referenced_locally`).
	// Here the one-shot read is the intent, and untracking it both states that
	// and clears the warning.
	let unit: TimeUnit = $state(untrack(() => startingUnit));

	const UNITS: TimeUnit[] = ['ms', 's', 'min'];
	let significantDigits = $state(2);

	// All five of these were `$:` statements and are plain derivations - each
	// computes a value from props and state and writes nothing back - so they
	// convert to `$derived` rather than to effects.
	const stepMs = $derived(step ?? STEP_MS[unit]);

	const nextUnit = $derived(UNITS[(UNITS.indexOf(unit) + 1) % UNITS.length]);

	const displayValue = $derived.by(() => {
		const conversionFactor = TO_MS[unit];
		const converted = value / conversionFactor;
		const precision = significantDigits > 0 ? significantDigits : 2;
		return Number(converted.toFixed(precision));
	});

	function handleInputChange(event: Event) {
		const input = event.target as HTMLInputElement;
		const inputValue = input.value;

		significantDigits = inputValue.split('.')[1]?.length ?? 0;

		const numericValue = Number(inputValue);
		if (!isNaN(numericValue)) {
			const valueInMs = numericValue * TO_MS[unit];
			value = valueInMs;
		}
	}

	function handleUnitChange() {
		unit = UNITS[(UNITS.indexOf(unit) + 1) % UNITS.length];
	}

	function decimalsFor(n: number) {
		return String(n).split('.')[1]?.length ?? 0;
	}

	function applyStep(valueInMs: number) {
		const newDisplay = valueInMs / TO_MS[unit];
		// Keep enough precision for the stepped value to survive the toFixed above.
		significantDigits = Math.max(significantDigits, decimalsFor(newDisplay));
		value = valueInMs;
	}

	function increment() {
		const newValue = value + stepMs;
		if (maxValue === null || newValue / TO_MS[unit] <= maxValue) {
			applyStep(newValue);
		}
	}

	function decrement() {
		const newValue = value - stepMs;
		const min = minValue !== null ? minValue : 0;
		if (newValue / TO_MS[unit] >= min) {
			applyStep(newValue);
		}
	}

	const inputWidth = $derived(`${Math.max(String(displayValue).length * 0.6 + 1.2, 3)}em`);

	const isInvalid = $derived(maxValue !== null && displayValue > maxValue);

	//TODO: reduce spacing at start of input field
</script>

<div class="flex items-center gap-2">
	{#if label}
		<label for={id} class="text-sm --main-text">
			{label}
		</label>
	{/if}

	<div class="flex items-center">
		<input
			type="number"
			value={displayValue}
			oninput={handleInputChange}
			{id}
			class="h-8 px-2 text-right
                   focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent
                   {isInvalid ? 'border-red-500' : ''} {showArrows
				? 'rounded-l-md'
				: 'rounded-l-lg'}"
			style="width: {inputWidth}"
			step="any"
			min="0"
		/>

		{#if showArrows}
			<div class="flex flex-col">
				<button onclick={increment} class="arrow-button" aria-label="Increment">
					<ChevronUp size={14} />
				</button>
				<button onclick={decrement} class="arrow-button" aria-label="Decrement">
					<ChevronDown size={14} />
				</button>
			</div>
		{/if}

		<button
			type="button"
			onclick={handleUnitChange}
			class="h-8 px-2 {highlightColor} text-white w-[40px] text-center
                   relative group hover:{highlightColor} focus:outline-none
                   focus:ring-2 focus:ring-blue-500 focus:ring-offset-2
                   {showArrows ? 'rounded-r-md' : 'rounded-r-lg'}"
		>
			{unit}
			<div
				class="absolute hidden group-hover:block bg-gray-800 text-white text-xs
                      rounded py-1 px-2 bottom-full mb-1 left-1/2 transform -translate-x-1/2
                      whitespace-nowrap z-10"
			>
				Click to convert to {UNIT_DISPLAY_NAMES[nextUnit]}
				<div
					class="absolute top-full left-1/2 transform -translate-x-1/2
                          border-4 border-transparent border-t-gray-800"
				></div>
			</div>
		</button>
	</div>
</div>

<style>
	input[type='number'] {
		-moz-appearance: textfield;
		appearance: textfield;
		background-color: var(--main);
		transition: background-color 0.3s;
	}

	input[type='number']:hover {
		background-color: var(--main-hover);
	}

	input[type='number']::-webkit-outer-spin-button,
	input[type='number']::-webkit-inner-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}

	input[type='number']:disabled {
		opacity: 0.7;
		cursor: not-allowed;
	}

	.arrow-button {
		display: flex;
		align-items: center;
		justify-content: center;
		width: 1.5rem;
		height: 1rem;
		background-color: var(--tertiary);
		padding: 0;
		transition: background-color 0.2s;
	}

	.arrow-button:hover {
		background-color: var(--tertiary-hover);
	}
</style>
