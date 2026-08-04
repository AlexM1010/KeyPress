<!-- DelayNode.svelte -->
<script lang="ts">
    import { Clock } from 'lucide-svelte';
    import NodeWrapper from './nodeComponents/NodeWrapper.svelte';
    import type { ComponentType } from 'svelte';
    import { Handle, Position } from "@xyflow/svelte";
    import type { HandleConfig, DelayNodeData } from '$lib/stores/flow';
    import TimeInput from './nodeComponents/TimeInput.svelte';
    import ButtonGroup from "./nodeComponents/ButtonGroup.svelte";
    import ButtonGroupItem from "./nodeComponents/ButtonGroupItem.svelte";

    export let id: string;
    export let title: string = 'Delay';
    export let icon: ComponentType = Clock;
    export let color: string = 'bg-gradient-to-r from-blue-500 to-blue-600';
    export let highlightColor: string = 'bg-blue-500';

    export let data: DelayNodeData = {
        id: '',
        type: 'DelayNode',
        position: { x: 0, y: 0 },
        data: {
            delayType: 'Fixed',
            time: 1000,
            minTime: 500,
            maxTime: 1500
        }
    };

    const handles: HandleConfig[] = [
        { id: "right", type: "source", position: Position.Right, offsetY: 50 },
        { id: "left", type: "target", position: Position.Left, offsetY: 50 },
    ];

    const DELAY_TYPES = ['Fixed', 'Random'];

    function updateDelayType(newType: string) {
        data.data.delayType = newType as 'Fixed' | 'Random';
    }

    $: {
        if (!data.data) {
            data.data = {
                delayType: 'Fixed',
                time: 1000,
                minTime: 500,
                maxTime: 1500
            };
        }
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
    bind:data
    on:duplicate
    on:delete
>
    <div class="space-y-4">
        <!-- Delay Type Selection -->
        <ButtonGroup variant="default">
            {#each DELAY_TYPES as type}
                <ButtonGroupItem 
                    value={type}
                    on:click={() => updateDelayType(type)}
                    active={data.data.delayType === type}
                    itemHighlightColor={highlightColor}
                >
                    {type}
                </ButtonGroupItem>
            {/each}
        </ButtonGroup>

        <!-- Fixed Delay Input -->
        {#if data.data.delayType === 'Fixed'}
            <TimeInput
                label="Time"
                bind:value={data.data.time}
                defaultValue={1000}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
        <!-- Random Delay Inputs -->
        {:else if data.data.delayType === 'Random'}
            <TimeInput
                label="Minimum Time"
                bind:value={data.data.minTime}
                defaultValue={500}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
            <TimeInput
                label="Maximum Time"
                bind:value={data.data.maxTime}
                defaultValue={1500}
                startingUnit="ms"
                minValue={0}
                highlightColor={highlightColor}
            />
        {/if}
    </div>
</NodeWrapper>
