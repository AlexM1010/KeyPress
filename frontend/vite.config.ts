import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		// Pinned to the IPv4 loopback rather than left as "localhost". Wails' dev
		// asset proxy dials 127.0.0.1, and on Windows "localhost" resolves to ::1
		// first - so Vite would bind IPv6-only and every request from the app
		// would fail with "connectex: No connection could be made", even though
		// the dev server is plainly running.
		host: '127.0.0.1',
		// `wails3 dev` passes the port it told the Go side about. Honouring it
		// here as well keeps `npm run dev` on the same port as the app expects.
		port: Number(process.env.WAILS_VITE_PORT) || 9245,
		strictPort: true
	}
});
