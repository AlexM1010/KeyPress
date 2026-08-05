<script lang="ts">
    import type { SvelteComponent } from 'svelte';
  
    export let label: string;
    export let type: string = "text";
    export let defaultValue: string = "";
    export let icon: typeof SvelteComponent<any> | null = null;
  </script>
  
  <div class="space-y-1.5">
<label for="inputField" class="block text-xs font-medium --main-text">{label}</label>
    <div class="relative">
      {#if icon}
        <div class="absolute left-3 top-1/2 transform -translate-y-1/2 --main-text">
          <svelte:component this={icon} class="w-4 h-4" />
        </div>
      {/if}
<input
  id="inputField"
  type={type}
  value={defaultValue}
        class="w-full pr-3 py-2 pl-10 text-sm bg-gray-50 border border-gray-200 rounded-lg focus:ring-2 focus:ring-blue-400 focus:border-transparent"
      />
    </div>
  </div>
  