<!-- MouseMoveNode.svelte -->
<script lang="ts">
    import { Position } from "@xyflow/svelte";
    import type { ComponentType } from 'svelte';
    import { Mouse, ChevronDown } from 'lucide-svelte';

    // Component Imports
    import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
    import Checkbox from "./nodeComponents/Checkbox.svelte";
    import NumberInput from './nodeComponents/NumberInput.svelte';
    import Slider from './nodeComponents/Slider.svelte';
    import TimeInput from './nodeComponents/TimeInput.svelte';
    import ButtonGroup from "./nodeComponents/ButtonGroup.svelte";
    import ButtonGroupItem from "./nodeComponents/ButtonGroupItem.svelte";
    import { markGraphEdited } from '$lib/stores/flow';
    import type { HandleConfig } from './types';

    // Type definitions
    type PositionType = 'Mouse' | 'Fixed';
    type PathType = 'Straight' | 'Human';
    type SpeedType = 'Instant' | 'Human';

    type Coordinates = {
        x: number;
        y: number;
    };

    // The node's data payload - this is what Svelte Flow hands to the `data` prop.
    type MouseMoveTaskData = {
        startPosition: {
            type: PositionType;
            coordinates: Coordinates;
        };
        endPosition: {
            type: PositionType;
            coordinates: Coordinates;
        };
        dragWhileMoving: boolean;
        speed: {
            type: SpeedType;
            value: number;
            randomize: boolean;
            variance: number;
        };
        pathType: PathType;
        customPath: Coordinates[];
    };

    // Props.
    // `data` is the very object the flow store holds for this node, so the edits
    // below (and the backfill further down) mutate it in place and are picked up by
    // `toObject()` on save. Never reassign `data` - that detaches the component
    // from the store and silently drops every edit.
    export let id: string;
    export let title: string = 'Mouse Move';
    export let icon: ComponentType = Mouse;
    export let color: string = 'bg-gradient-to-r from-green-500 to-green-600';
    export let highlightColor: string = 'bg-green-500';


    // Data initialization with default values
    export let data: MouseMoveTaskData = {
        startPosition: {
            type: 'Mouse',
            coordinates: { x: 0, y: 0 }
        },
        endPosition: {
            type: 'Fixed',
            coordinates: { x: 0, y: 0 }
        },
        dragWhileMoving: false,
        speed: {
            type: 'Instant',
            value: 500,
            randomize: false,
            variance: 20
        },
        pathType: 'Straight',
        customPath: []
    };

    // Reactive statement for data consistency
    $: {
        if (data?.startPosition == null) data.startPosition = { type: 'Mouse', coordinates: { x: 0, y: 0 } };
        if (data?.endPosition == null) data.endPosition = { type: 'Fixed', coordinates: { x: 0, y: 0 } };
        if (data?.dragWhileMoving == null) data.dragWhileMoving = false;
        if (data?.speed == null) {
            data.speed = {
                type: 'Instant',
                value: 500,
                randomize: false,
                variance: 20
            };
        }
        if (data?.pathType == null) data.pathType = 'Straight';
        if (data?.customPath == null) data.customPath = [];
    }


    // Local UI state
    let showMovementSettings = false;

    // Debug logging
    $: console.log('MouseMoveNode data:', JSON.stringify(data));

    /**
     * Node connection handle configuration
     */
    const handles: HandleConfig[] = [
        { id: "right", type: "source", position: Position.Right, offsetY: 50 },
        { id: "left", type: "target", position: Position.Left, offsetY: 50 },
    ];

    // Available options
    const PATH_TYPES: PathType[] = ['Straight', 'Human'];
    const SPEED_TYPES: SpeedType[] = ['Instant', 'Human'];
    const POSITION_TYPES: PositionType[] = ['Fixed', 'Mouse'];

    // Constants
    const CONFIG = {
        POSITION: {
            MIN: -10000,
            MAX: 10000
        },
        SPEED: {
            DEFAULT: 500,
            MIN: 100,
            MAX: 5000
        },
        VARIANCE: {
            DEFAULT: 20,
            MIN: 0,
            MAX: 100
        }
    } as const;

    /**
     * The other half of the by-reference contract: mutating `data` puts the edit
     * in the store, this announces it. Every binding and handler in this file
     * assigns to a property of `data` - the deep ones
     * (`data.startPosition.coordinates.x`) included, because Svelte invalidates
     * the whole `data` variable for an assignment anywhere along that path -
     * which re-runs this; see `markGraphEdited`.
     *
     * Declared last, as it is in every node component: a reactive statement that
     * writes `data` through a function call is invisible to the compiler, so
     * source order is all that keeps its write on the announcing side of this
     * one (see the longer note in `KeyPressNode.svelte`). The consistency block
     * above writes `data` directly, which the compiler *can* see, so that one is
     * ordered before this whatever the source says.
     *
     * A node opened from a file that predates one of those fields therefore
     * announces one edit as it mounts. That is harmless: the unsaved-changes
     * baseline is taken a frame after the nodes mount.
     */
    $: {
        data;
        markGraphEdited();
    }

    // Event handlers
    // Svelte Flow's NodeWrapper passes a fixed prop set (selected, isConnectable,
    // positionAbsoluteX, ...) to every custom node. Referencing $$restProps silences
    // the "created with unknown prop" warnings for the ones we don't declare.
    $$restProps;
</script>

<NodeWrapper
    {id}
    {icon}
    {title}
    {color}
    type="Move"
    {handles}
>
    <div class="grid gap-6">
        <!-- Start Position Configuration -->
        <div class="grid gap-4">
            <h3 class="text-sm font-medium --main-text">Start Position</h3>
            <ButtonGroup variant="default">
                <ButtonGroupItem 
                    value="Fixed"
                    on:click={() => data.startPosition.type = 'Fixed'}
                    active={data.startPosition.type === 'Fixed'}
                    itemHighlightColor={highlightColor}
                >
                    Fixed
                </ButtonGroupItem>
                <ButtonGroupItem 
                    value="Mouse"
                    on:click={() => data.startPosition.type = 'Mouse'}
                    active={data.startPosition.type === 'Mouse'}
                    disabled={data.endPosition.type === 'Mouse'}
                    itemHighlightColor={highlightColor}
                >
                    Mouse
                </ButtonGroupItem>
            </ButtonGroup>
            
            {#if data.startPosition.type === 'Fixed'}
                <div class="grid grid-cols-2 gap-4">
                    <NumberInput
                        label="X"
                        bind:value={data.startPosition.coordinates.x}
                        minValue={CONFIG.POSITION.MIN}
                        maxValue={CONFIG.POSITION.MAX}
                    />
                    <NumberInput
                        label="Y"
                        bind:value={data.startPosition.coordinates.y}
                        minValue={CONFIG.POSITION.MIN}
                        maxValue={CONFIG.POSITION.MAX}
                    />
                </div>
            {/if}
        </div>

        <!-- End Position Configuration -->
        <div class="grid gap-4">
            <h3 class="text-sm font-medium --main-text">End Position</h3>
            <ButtonGroup variant="default">
                <ButtonGroupItem 
                    value="Fixed"
                    on:click={() => data.endPosition.type = 'Fixed'}
                    active={data.endPosition.type === 'Fixed'}
                    itemHighlightColor={highlightColor}
                >
                    Fixed
                </ButtonGroupItem>
                <ButtonGroupItem 
                    value="Mouse"
                    on:click={() => data.endPosition.type = 'Mouse'}
                    active={data.endPosition.type === 'Mouse'}
                    disabled={data.startPosition.type === 'Mouse'}
                    itemHighlightColor={highlightColor}
                >
                    Mouse
                </ButtonGroupItem>
            </ButtonGroup>
            
            {#if data.endPosition.type === 'Fixed'}
                <div class="grid grid-cols-2 gap-4">
                    <NumberInput
                        label="X"
                        bind:value={data.endPosition.coordinates.x}
                        minValue={CONFIG.POSITION.MIN}
                        maxValue={CONFIG.POSITION.MAX}
                    />
                    <NumberInput
                        label="Y"
                        bind:value={data.endPosition.coordinates.y}
                        minValue={CONFIG.POSITION.MIN}
                        maxValue={CONFIG.POSITION.MAX}
                    />
                </div>
            {/if}
        </div>

        <!-- Advanced Movement Settings -->
        <div class="border-t pt-2" style="border-color: var(--secondary-text);">
            <button
                class="flex items-center justify-between w-full text-sm --main-text hover:--main-text transition-colors"
                on:click={() => showMovementSettings = !showMovementSettings}
                aria-expanded={showMovementSettings}
            >
                <span>Movement Settings</span>
                <ChevronDown
                    class="w-4 h-4 transition-transform duration-200"
                    style={showMovementSettings ? "transform: rotate(180deg)" : ""}
                />
            </button>

            {#if showMovementSettings}
                <div class="mt-4 grid gap-6">
                    <!-- Drag Option -->
                    <Checkbox
                        label="Drag"
                        bind:checked={data.dragWhileMoving}
                        highlightColor={highlightColor}
                    />

                    <!-- Move Speed Configuration -->
                    <div class="grid gap-4">
                        <h4 class="text-sm font-medium --main-text">Move Speed</h4>
                        <ButtonGroup variant="default">
                            {#each SPEED_TYPES as type}
                                <ButtonGroupItem 
                                    value={type}
                                    on:click={() => data.speed.type = type}
                                    active={data.speed.type === type}
                                    itemHighlightColor={highlightColor}
                                >
                                    {type}
                                </ButtonGroupItem>
                            {/each}
                        </ButtonGroup>

                        {#if data.speed.type === 'Human'}
                            <TimeInput
                                label="Speed"
                                bind:value={data.speed.value}
                                defaultValue={CONFIG.SPEED.DEFAULT}
                                startingUnit="ms"
                                highlightColor={highlightColor}
                            />
                            <div class="flex items-center gap-4">
                                <Checkbox
                                    label="Randomize"
                                    bind:checked={data.speed.randomize}
                                    highlightColor={highlightColor}
                                />
                                {#if data.speed.randomize}
                                    <div class="flex-1">
                                        <Slider
                                            label="Variance"
                                            unit="%"
                                            bind:value={data.speed.variance}
                                            min={CONFIG.VARIANCE.MIN}
                                            max={CONFIG.VARIANCE.MAX}
                                            defaultValue={CONFIG.VARIANCE.DEFAULT}
                                        />
                                    </div>
                                {/if}
                            </div>
                        {/if}
                    </div>

                    <!-- Path Type Configuration -->
                    <div class="grid gap-4">
                        <h4 class="text-sm font-medium --main-text">Path Type</h4>
                        <ButtonGroup variant="default">
                            {#each PATH_TYPES as type}
                                <ButtonGroupItem 
                                    value={type}
                                    on:click={() => data.pathType = type}
                                    active={data.pathType === type}
                                    itemHighlightColor={highlightColor}
                                >
                                    {type}
                                </ButtonGroupItem>
                            {/each}
                        </ButtonGroup>
                    </div>
                </div>
            {/if}
        </div>
    </div>
</NodeWrapper>
