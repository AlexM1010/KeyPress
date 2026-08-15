<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { ModeWatcher } from 'mode-watcher';
	import ThemeToggle from './ThemeToggle.svelte';
	import Logo from './Logo.svelte';
	import '$lib/index.scss';
	import './navbar.css';
</script>

<ModeWatcher />

<!-- Navbar -->
<div class="navbar-container text-white">
	<!-- Left side of navbar -->
	<div class="navbar-left">
		<Logo customClass="logo" />
		<button class="nav-btn" on:click={() => goto(resolve('/'))}> Workspace </button>
		<button class="nav-btn" on:click={() => goto(resolve('/projects'))}> Projects </button>
	</div>
	<!-- Right side of navbar -->
	<div class="flex items-center">
		<ThemeToggle />
	</div>
</div>

<slot />
