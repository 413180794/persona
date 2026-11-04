// The part of noVNC's RFB (core/rfb.js, @novnc/novnc 1.7) the live screen uses; the package ships no types.
declare module "@novnc/novnc" {
  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string, options?: { shared?: boolean; credentials?: { username?: string; password?: string }; wsProtocols?: string[] });
    viewOnly: boolean;
    scaleViewport: boolean;
    clipViewport: boolean;
    resizeSession: boolean;
    focusOnClick: boolean;
    background: string;
    qualityLevel: number;
    compressionLevel: number;
    sendCredentials(credentials: { username?: string; password?: string }): void;
    disconnect(): void;
    focus(): void;
  }
}
