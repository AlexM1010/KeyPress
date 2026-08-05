<!-- frontend\src\lib\components\customNodes\TimeInput.svelte -->
<script lang="ts">
    import { ChevronUp, ChevronDown } from 'lucide-svelte';
    import "$lib/index.scss";

    type TimeUnit = 'ms' | 's' | 'min';

    interface ConversionFactors {
        ms: number;
        s: number;
        min: number;
    }

    const TO_MS: ConversionFactors = {
        'ms': 1,
        's': 1000,
        'min': 60 * 1000
    };

    const UNIT_DISPLAY_NAMES: Record<TimeUnit, string> = {
        'ms': 'milliseconds',
        's': 'seconds',
        'min': 'minutes'
    };

    export let label = '';
    export let defaultValue = 1;
    export let value = defaultValue * 1000;
    export let highlightColor: string = 'bg-grey-500';
    export let startingUnit: TimeUnit = 's';
    let unit: TimeUnit = startingUnit;

    export let showArrows: boolean = true;
    export let step: number = 1;
    export let minValue: number | null = null;
    export let maxValue: number | null = null;

    const UNITS: TimeUnit[] = ['ms', 's', 'min'];
    let significantDigits = 2;

    let displayValue: number;
    let inputWidth: string;
    let isInvalid: boolean = false;

    $: nextUnit = UNITS[(UNITS.indexOf(unit) + 1) % UNITS.length];

    $: {
        const conversionFactor = TO_MS[unit];
        const converted = value / conversionFactor;
        const precision = significantDigits > 0 ? significantDigits : 2;
        displayValue = Number(converted.toFixed(precision));
    }

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

    function increment() {
        let newValue = displayValue + step;
        if (maxValue === null || newValue <= maxValue) {
            const valueInMs = newValue * TO_MS[unit];
            value = valueInMs;
        }
    }

    function decrement() {
        let newValue = displayValue - step;
        const min = minValue !== null ? minValue : 0;
        if (newValue >= min) {
            const valueInMs = newValue * TO_MS[unit];
            value = valueInMs;
        }
    }

    $: inputWidth = `${Math.max(String(displayValue).length * 0.6 + 1.2, 3)}em`;

    $: isInvalid = maxValue !== null && displayValue > maxValue;

    //TODO: reduce spacing at start of input field
</script>

<div class="flex items-center gap-2">
    {#if label}
        <label 
            for="time-input" 
            class="text-sm --main-text"
        >
            {label}
        </label>
    {/if}
    
    <div class="flex items-center">
        <input 
            type="number"
            value={displayValue}
            on:input={handleInputChange}
            id="time-input" 
            class="h-8 px-2 text-right
                   focus:outline-none focus:ring-2 focus:ring-blue-500 focus:border-transparent 
                   {isInvalid ? 'border-red-500' : ''} {showArrows ? 'rounded-l-md' : 'rounded-l-lg'}"
            style="width: {inputWidth}"
            step="any"
            min="0"
        />

        {#if showArrows}
            <div class="flex flex-col">
                <button 
                    on:click={increment}
                    class="arrow-button"
                    aria-label="Increment"
                >
                    <ChevronUp size={14} />
                </button>
                <button 
                    on:click={decrement}
                    class="arrow-button"
                    aria-label="Decrement"
                >
                    <ChevronDown size={14} />
                </button>
            </div>
        {/if}

        <button 
            type="button"
            on:click={handleUnitChange} 
            class="h-8 px-2 {highlightColor} text-white w-[40px] text-center 
                   relative group hover:{highlightColor} focus:outline-none 
                   focus:ring-2 focus:ring-blue-500 focus:ring-offset-2 
                   {showArrows ? 'rounded-r-md' : 'rounded-r-lg'}"
        >
            {unit}
            <div class="absolute hidden group-hover:block bg-gray-800 text-white text-xs 
                      rounded py-1 px-2 bottom-full mb-1 left-1/2 transform -translate-x-1/2 
                      whitespace-nowrap z-10">
                Click to convert to {UNIT_DISPLAY_NAMES[nextUnit]}
                <div class="absolute top-full left-1/2 transform -translate-x-1/2 
                          border-4 border-transparent border-t-gray-800">
                </div>
            </div>
        </button>
    </div>
</div>

<style>
    input[type="number"] {
        -moz-appearance: textfield;
        appearance: textfield;
        background-color: var(--main);
        transition: background-color 0.3s;
    }

    input[type="number"]:hover {
        background-color: var(--main-hover);
    }

    input[type="number"]::-webkit-outer-spin-button,
    input[type="number"]::-webkit-inner-spin-button {
        -webkit-appearance: none;
        margin: 0;
    }

    input[type="number"]:disabled {
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