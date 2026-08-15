<!-- frontend\src\lib\components\customNodes\NumberInput.svelte -->
<script lang="ts">
	import { ChevronUp, ChevronDown } from 'lucide-svelte';

	export let label: string = '';

	// The DOM id of the number box, tying it to its <label for=...>. It used to
	// be the hard-coded literal "number-input", so every instance on the page
	// rendered the same id: the Wait For Color node alone puts three of these in
	// one node, so the document had duplicate ids and *every* label resolved to
	// the first box - clicking "Tolerance" focused X. A fresh unique value per
	// instance is the same fix Select.svelte and Slider.svelte already carry;
	// callers that want a stable, predictable id can still pass one in.
	export let id: string = `number-input-${crypto.randomUUID()}`;

	export let value: number = 0;
	export let minValue: number | null = null;
	export let maxValue: number | null = null;
	export let step: number = 1;
	export let showArrows: boolean = true;

	let isInvalid: boolean;

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

	$: inputWidth = `${Math.max(String(value).length * 0.6 + 1, 1)}em`;
	$: if (maxValue !== null && value > maxValue) {
		value = maxValue;
		isInvalid = true;
	} else {
		isInvalid = false;
	}
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
					<button on:click={increment} class="arrow-button rounded-tr-md" aria-label="Increment">
						<ChevronUp size={14} />
					</button>
					<button on:click={decrement} class="arrow-button rounded-br-md" aria-label="Decrement">
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
