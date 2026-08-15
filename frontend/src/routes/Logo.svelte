<script lang="ts">
	export let customClass: string = ''; // Accept a `customClass` prop
</script>

<div class={`logo-container ${customClass}`}>
	<img
		src="/Keypress-Logo.svg"
		alt="Keypress Logo"
		class="h-8 w-auto max-w-full object-contain ml-4 mr-6"
		style="user-select: none; no-drag: true;"
	/>
</div>
