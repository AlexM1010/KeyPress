// A macro's graph is read from the Go runtime, which only exists inside the
// running Wails app. There is nothing to resolve at build time, so this route
// is neither prerendered nor server-rendered - the page loads its own data in
// the browser. adapter-static serves it through the SPA fallback.
export const prerender = false;
export const ssr = false;
