export { };

// Define the Wails runtime interface
export interface WailsRuntime {
    EventsOn: (eventName: string, callback: (...args: any[]) => void) => void;
    EventsOff: (eventName: string) => void;
    EventsEmit(eventName: string, ...data: any[]): void;
}

// Single consolidated Window interface declaration.
// The Go bindings themselves are consumed through the generated
// `$lib/wailsjs/go/backend/App` module, so no hand-written `Backend` shape is declared
// here - only the runtime, which has no generated typings.
declare global {
    interface Window {
        runtime: WailsRuntime;
    }
}