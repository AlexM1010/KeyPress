<!-- frontend\src\lib\components\customNodes\NumberInput.svelte -->
<script lang="ts">
	import { ChevronUp, ChevronDown } from 'lucide-svelte';

	type Props = {
		label?: string;
		id?: string;
		value?: number;
		minValue?: number | null;
		maxValue?: number | null;
		step?: number;
		showArrows?: boolean;
	};

	let {
		label = '',
		// The DOM id of the number box, tying it to its <label for=...>. It used to
		// be the hard-coded literal "number-input", so every instance on the page
		// rendered the same id: the Wait For Color node alone puts three of these in
		// one node, so the document had duplicate ids and *every* label resolved to
		// the first box - clicking "Tolerance" focused X. A fresh unique value per
		// instance is the same fix Select.svelte and Slider.svelte already carry;
		// callers that want a stable, predictable id can still pass one in.
		id = `number-input-${crypto.randomUUID()}`,
		// `$bindable` because the clamp below writes this prop from inside, and
		// every caller binds it to a field of the node's `data` payload. That write
		// travelling back up is load-bearing rather than cosmetic: a tolerance typed
		// past 255 has to reach `data`, not merely the box on screen, because the
		// saved file is what the backend reads.
		value = $bindable(0),
		minValue = null,
		maxValue = null,
		step = 1,
		showArrows = true
	}: Props = $props();

	let isInvalid = $state(false);

	function increment() {
		if (maxValue === null || value + step <= maxValue) {
			value += step;
		}
	}

	function decrement() {
		if (minValue === null || value - step >= minValue) {
			value -= step;
		}
	}

	const inputWidth = $derived(`${Math.max(String(value).length * 0.6 + 1, 1)}em`);

	// `$effect.pre` rather than a plain `$effect`, because the reactive block this
	// replaces ran *before* the DOM was updated: a post-render effect would paint
	// the over-max value for a frame and only then correct it.
	//
	// The effect reads `value` and writes it, which is safe here only because the
	// clamp is idempotent - the re-run it schedules finds the value already at
	// `maxValue` and writes nothing, so it settles in one extra pass rather than
	// looping into `effect_update_depth_exceeded`.
	//
	// `isInvalid` therefore ends up permanently false whenever `maxValue` is set,
	// which is exactly what the `$:` block did too: it set the flag in the same
	// breath as the correction that makes the condition false again, so the
	// re-run immediately took the `else`. That is preserved rather than fixed -
	// it is a pre-existing quirk of the error styling, not part of this migration.
	$effect.pre(() => {
		if (maxValue !== null && value > maxValue) {
			value = maxValue;
			isInvalid = true;
		} else {
			isInvalid = false;
		}
	});
</script>

<div class="flex flex-col">
	<div class="flex items-center">
		{#if label}
			<label for={id} class="text-sm --text-main mr-2">{label}</label>
		{/if}
		<div class="flex">
			<input
				type="number"
				bind:value
				{id}
				class="h-8 px-2 text-right
                    {isInvalid ? 'input-error' : ''} 
                    {showArrows ? 'rounded-l-md' : 'rounded-md'}"
				style="width: {inputWidth}"
				min={minValue}
				max={maxValue}
				{step}
			/>
			{#if showArrows}
				<div class="flex flex-col">
					<button onclick={increment} class="arrow-button rounded-tr-md" aria-label="Increment">
						<ChevronUp size={14} />
					</button>
					<button onclick={decrement} class="arrow-button rounded-br-md" aria-label="Decrement">
						<ChevronDown size={14} />
					</button>
				</div>
			{/if}
		</div>
	</div>
</div>

<style>
	input[type='number']::-webkit-outer-spin-button,
	input[type='number']::-webkit-inner-spin-button {
		-webkit-appearance: none;
		margin: 0;
	}

	input[type='number'] {
		appearance: textfield;
		-moz-appearance: textfield;
		text-align: center;
		background-color: var(--main);
		transition: background-color 0.3s;
	}

	input[type='number']:hover {
		background-color: var(--main-hover);
	}

	.input-error {
		border-width: 2px;
		border-color: red;
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
		background-color: #d1d5db;
	}
</style>
