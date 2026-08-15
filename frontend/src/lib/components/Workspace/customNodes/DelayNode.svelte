<!-- DelayNode.svelte -->
<script lang="ts">
    import { Clock } from 'lucide-svelte';
    import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
    import type { ComponentType } from 'svelte';
    import { Handle, Position } from "@xyflow/svelte";
    import type { HandleConfig, DelayNodeData } from '$lib/stores/flow.svelte';
    import TimeInput from './nodeComponents/TimeInput.svelte';
    import ButtonGroup from "./nodeComponents/ButtonGroup.svelte";
    import ButtonGroupItem from "./nodeComponents/ButtonGroupItem.svelte";

    export let id: string;
    export let title: string = 'Delay';
    export let icon: ComponentType = Clock;
    export let color: string = 'bg-gradient-to-r from-blue-500 to-blue-600';
    export let highlightColor: string = 'bg-blue-500';

    const DEFAULT_DATA: DelayNodeData = {
        delayType: 'Fixed',
        time: 1000,
        minTime: 500,
        maxTime: 1500
    };

    // Svelte Flow passes the node's data payload here, not the whole node - and it
    // is the *same object* the graph holds, so every edit below lands in the graph
    // by reference and is what gets saved.
    //
    // That reference is the whole of the contract now. The graph's nodes are deep
    // `$state` (see `$lib/stores/flow.svelte`), so `data.time = 2500` is an
    // ordinary tracked write and the edit *is* its own notification - nothing has
    // to be announced afterwards.
    export let data: DelayNodeData = { ...DEFAULT_DATA };

    // Persisted nodes can predate newer fields, so backfill whatever is missing
    // without clobbering saved values. This has to mutate `data` in place:
    // reassigning it (`data = { ...DEFAULT_DATA, ...data }`) would detach this
    // component from the store's object and silently discard every later edit.
    Object.assign(data, { ...DEFAULT_DATA, ...data });

    const handles: HandleConfig[] = [
        { id: "right", type: "source", position: Position.Right, offsetY: 50 },
        { id: "left", type: "target", position: Position.Left, offsetY: 50 },
    ];

    const DELAY_TYPES = ['Fixed', 'Random'];

    function updateDelayType(newType: string) {
        data.delayType = newType as 'Fixed' | 'Random';
    }

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
    type="Delay"
    {handles}
>
    <div class="space-y-4">
        <!-- Delay Type Selection -->
        <ButtonGroup variant="default">
            {#each DELAY_TYPES as type}
                <ButtonGroupItem 
                    value={type}
                    on:click={() => updateDelayType(type)}
                    active={data.delayType === type}
                    itemHighlightColor={highlightColor}
                >
                    {type}
                </ButtonGroupItem>
            {/each}
        </ButtonGroup>

        <!-- Fixed Delay Input -->
        {#if data.delayType === 'Fixed'}
            <TimeInput
                label="Time"
                bind:value={data.time}
                defaultValue={1000}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
        <!-- Random Delay Inputs -->
        {:else if data.delayType === 'Random'}
            <TimeInput
                label="Minimum Time"
                bind:value={data.minTime}
                defaultValue={500}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
            <TimeInput
                label="Maximum Time"
                bind:value={data.maxTime}
                defaultValue={1500}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
        {/if}
    </div>
</NodeWrapper>
