/* Persona Android redesign preview.
   Each screen is a function returning HTML; the hash (#screen=…&theme=…&w=…&only=phone) picks one, so screenshots
   can be scripted. Every name, address and number on these screens is made up. */
(() => {
  'use strict';

  // Material icons, 24×24 paths.
  const P = {
    add: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z',
    add_comment: 'M22 4c0-1.1-.9-2-2-2H4c-1.1 0-1.99.9-1.99 2L2 16c0 1.1.9 2 2 2h14l4 4V4zm-5 7h-4v4h-2v-4H7V9h4V5h2v4h4v2z',
    alarm: 'M22 5.72l-4.6-3.86-1.29 1.53 4.6 3.86L22 5.72zM7.88 3.39L6.6 1.86 2 5.71l1.29 1.53 4.59-3.85zM12.5 8H11v6l4.75 2.85.75-1.23-4-2.37V8zM12 4c-4.97 0-9 4.03-9 9s4.02 9 9 9c4.97 0 9-4.03 9-9s-4.03-9-9-9zm0 16c-3.87 0-7-3.13-7-7s3.13-7 7-7 7 3.13 7 7-3.13 7-7 7z',
    arrow_back: 'M20 11H7.83l5.59-5.59L12 4l-8 8 8 8 1.41-1.41L7.83 13H20v-2z',
    assignment: 'M19 3h-4.18C14.4 1.84 13.3 1 12 1c-1.3 0-2.4.84-2.82 2H5c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2zm-7 0c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1zm2 14H7v-2h7v2zm3-4H7v-2h10v2zm0-4H7V7h10v2z',
    auto_awesome: 'M19 9l1.25-2.75L23 5l-2.75-1.25L19 1l-1.25 2.75L15 5l2.75 1.25L19 9zm-7.5.5L9 4 6.5 9.5 1 12l5.5 2.5L9 20l2.5-5.5L17 12l-5.5-2.5zM19 15l-1.25 2.75L15 19l2.75 1.25L19 23l1.25-2.75L23 19l-2.75-1.25L19 15z',
    battery: 'M15.67 4H14V2h-4v2H8.33C7.6 4 7 4.6 7 5.33v15.33C7 21.4 7.6 22 8.33 22h7.33c.74 0 1.34-.6 1.34-1.33V5.33C17 4.6 16.4 4 15.67 4z',
    book: 'M18 2H6c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zM6 4h5v8l-2.5-1.5L6 12V4z',
    chat: 'M20 2H4c-1.1 0-1.99.9-1.99 2L2 22l4-4h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zM6 9h12v2H6V9zm8 5H6v-2h8v2zm4-6H6V6h12v2z',
    check: 'M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z',
    check_circle: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z',
    chevron_right: 'M10 6L8.59 7.41 13.17 12l-4.58 4.59L10 18l6-6z',
    close: 'M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z',
    copy: 'M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z',
    description: 'M14 2H6c-1.1 0-1.99.9-1.99 2L4 20c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm2 16H8v-2h8v2zm0-4H8v-2h8v2zm-3-5V3.5L18.5 9H13z',
    desktop: 'M20 2H4c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h7v2H8v2h8v-2h-3v-2h7c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm0 14H4V4h16v12z',
    dns: 'M20 13H4c-.55 0-1 .45-1 1v6c0 .55.45 1 1 1h16c.55 0 1-.45 1-1v-6c0-.55-.45-1-1-1zM7 19c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zM20 3H4c-.55 0-1 .45-1 1v6c0 .55.45 1 1 1h16c.55 0 1-.45 1-1V4c0-.55-.45-1-1-1zM7 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2z',
    edit: 'M3 17.25V21h3.75L17.81 9.94l-3.75-3.75L3 17.25zM20.71 7.04c.39-.39.39-1.02 0-1.41l-2.34-2.34c-.39-.39-1.02-.39-1.41 0l-1.83 1.83 3.75 3.75 1.83-1.83z',
    error: 'M11 15h2v2h-2zm0-8h2v6h-2zm.99-5C6.47 2 2 6.48 2 12s4.47 10 9.99 10C17.52 22 22 17.52 22 12S17.52 2 11.99 2zM12 20c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8z',
    event: 'M17 12h-5v5h5v-5zM16 1v2H8V1H6v2H5c-1.11 0-1.99.9-1.99 2L3 19c0 1.1.89 2 2 2h14c1.1 0 2-.9 2-2V5c0-1.1-.9-2-2-2h-1V1h-2zm3 18H5V8h14v11z',
    expand_less: 'M12 8l-6 6 1.41 1.41L12 10.83l4.59 4.58L18 14z',
    expand_more: 'M16.59 8.59L12 13.17 7.41 8.59 6 10l6 6 6-6z',
    extension: 'M20.5 11H19V7c0-1.1-.9-2-2-2h-4V3.5C13 2.12 11.88 1 10.5 1S8 2.12 8 3.5V5H4c-1.1 0-1.99.9-1.99 2v3.8H3.5c1.49 0 2.7 1.21 2.7 2.7s-1.21 2.7-2.7 2.7H2V20c0 1.1.9 2 2 2h3.8v-1.5c0-1.49 1.21-2.7 2.7-2.7 1.49 0 2.7 1.21 2.7 2.7V22H17c1.1 0 2-.9 2-2v-4h1.5c1.38 0 2.5-1.12 2.5-2.5S21.88 11 20.5 11z',
    flash: 'M6 2h12v3H6zm0 5v1l2 3v11h8V11l2-3V7H6zm6 8.5c-.83 0-1.5-.67-1.5-1.5s.67-1.5 1.5-1.5 1.5.67 1.5 1.5-.67 1.5-1.5 1.5z',
    help: 'M11 18h2v-2h-2v2zm1-16C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm0-14c-2.21 0-4 1.79-4 4h2c0-1.1.9-2 2-2s2 .9 2 2c0 2-3 1.75-3 5h2c0-2.25 3-2.5 3-5 0-2.21-1.79-4-4-4z',
    history: 'M13 3c-4.97 0-9 4.03-9 9H1l3.89 3.89.07.14L9 12H6c0-3.87 3.13-7 7-7s7 3.13 7 7-3.13 7-7 7c-1.93 0-3.68-.79-4.94-2.06l-1.42 1.42C8.27 19.99 10.51 21 13 21c4.97 0 9-4.03 9-9s-4.03-9-9-9zm-1 5v5l4.28 2.54.72-1.21-3.5-2.08V8H12z',
    inbox: 'M19 3H4.99c-1.11 0-1.98.89-1.98 2L3 19c0 1.1.88 2 1.99 2H19c1.1 0 2-.9 2-2V5c0-1.11-.9-2-2-2zm0 12h-4c0 1.66-1.35 3-3 3s-3-1.34-3-3H4.99V5H19v10z',
    info: 'M11 7h2v2h-2zm0 4h2v6h-2zm1-9C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8z',
    laptop: 'M20 18c1.1 0 1.99-.9 1.99-2L22 6c0-1.1-.9-2-2-2H4c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2H0v2h24v-2h-4zM4 6h16v10H4V6z',
    layers: 'M11.99 18.54l-7.37-5.73L3 14.07l9 7 9-7-1.63-1.27-7.38 5.74zM12 16l7.36-5.73L21 9l-9-7-9 7 1.63 1.27L12 16z',
    lightbulb: 'M9 21c0 .55.45 1 1 1h4c.55 0 1-.45 1-1v-1H9v1zm3-19C8.14 2 5 5.14 5 9c0 2.38 1.19 4.47 3 5.74V17c0 .55.45 1 1 1h6c.55 0 1-.45 1-1v-2.26c1.81-1.27 3-3.36 3-5.74 0-3.86-3.14-7-7-7z',
    link: 'M3.9 12c0-1.71 1.39-3.1 3.1-3.1h4V7H7c-2.76 0-5 2.24-5 5s2.24 5 5 5h4v-1.9H7c-1.71 0-3.1-1.39-3.1-3.1zM8 13h8v-2H8v2zm9-6h-4v1.9h4c1.71 0 3.1 1.39 3.1 3.1s-1.39 3.1-3.1 3.1h-4V17h4c2.76 0 5-2.24 5-5s-2.24-5-5-5z',
    list: 'M4 10.5c-.83 0-1.5.67-1.5 1.5s.67 1.5 1.5 1.5 1.5-.67 1.5-1.5-.67-1.5-1.5-1.5zm0-6c-.83 0-1.5.67-1.5 1.5S3.17 7.5 4 7.5 5.5 6.83 5.5 6 4.83 4.5 4 4.5zm0 12c-.83 0-1.5.68-1.5 1.5s.68 1.5 1.5 1.5 1.5-.68 1.5-1.5-.67-1.5-1.5-1.5zM7 19h14v-2H7v2zm0-6h14v-2H7v2zm0-8v2h14V5H7z',
    lock: 'M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z',
    loop: 'M7 7h10v3l4-4-4-4v3H5v6h2V7zm10 10H7v-3l-4 4 4 4v-3h12v-6h-2v4z',
    mail: 'M20 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 4l-8 5-8-5V6l8 5 8-5v2z',
    menu: 'M3 18h18v-2H3v2zm0-5h18v-2H3v2zm0-7v2h18V6H3z',
    mic: 'M12 14c1.66 0 2.99-1.34 2.99-3L15 5c0-1.66-1.34-3-3-3S9 3.34 9 5v6c0 1.66 1.34 3 3 3zm5.3-3c0 3-2.54 5.1-5.3 5.1S6.7 14 6.7 11H5c0 3.41 2.72 6.23 6 6.72V21h2v-3.28c3.28-.48 6-3.3 6-6.72h-1.7z',
    more: 'M12 8c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm0 2c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm0 6c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z',
    notifications: 'M12 22c1.1 0 2-.9 2-2h-4c0 1.1.89 2 2 2zm6-6v-5c0-3.07-1.64-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68C7.63 5.36 6 7.92 6 11v5l-2 2v1h16v-1l-2-2z',
    notifications_off: 'M20 18.69L7.84 6.14 5.27 3.49 4 4.76l2.8 2.8v.01c-.52.99-.8 2.16-.8 3.42v5l-2 2v1h13.73l2 2L21 19.72l-1-1.03zM12 22c1.11 0 2-.89 2-2h-4c0 1.11.89 2 2 2zm6-7.32V11c0-3.08-1.64-5.64-4.5-6.32V4c0-.83-.67-1.5-1.5-1.5s-1.5.67-1.5 1.5v.68c-.15.03-.29.08-.42.12-.1.03-.2.07-.3.11h-.01c-.01 0-.01 0-.02.01-.23.09-.46.2-.68.31 0 0-.01 0-.01.01L18 14.68z',
    open_full: 'M21 11V3h-8l3.29 3.29-10 10L3 13v8h8l-3.29-3.29 10-10z',
    open_new: 'M19 19H5V5h7V3H5c-1.11 0-2 .9-2 2v14c0 1.1.89 2 2 2h14c1.1 0 2-.9 2-2v-7h-2v7zM14 3v2h3.59l-9.83 9.83 1.41 1.41L19 6.41V10h2V3h-7z',
    pending: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zM7 10.5c-.83 0-1.5.67-1.5 1.5s.67 1.5 1.5 1.5 1.5-.67 1.5-1.5-.67-1.5-1.5-1.5zm5 0c-.83 0-1.5.67-1.5 1.5s.67 1.5 1.5 1.5 1.5-.67 1.5-1.5-.67-1.5-1.5-1.5zm5 0c-.83 0-1.5.67-1.5 1.5s.67 1.5 1.5 1.5 1.5-.67 1.5-1.5-.67-1.5-1.5-1.5z',
    phone: 'M16 1H8C6.34 1 5 2.34 5 4v16c0 1.66 1.34 3 3 3h8c1.66 0 3-1.34 3-3V4c0-1.66-1.34-3-3-3zm-2 20h-4v-1h4v1zm3.25-3H6.75V4h10.5v14z',
    play: 'M8 5v14l11-7z',
    power: 'M13 3h-2v10h2V3zm4.83 2.17l-1.42 1.42C17.99 7.86 19 9.81 19 12c0 3.87-3.13 7-7 7s-7-3.13-7-7c0-2.19 1.01-4.14 2.58-5.42L6.17 5.17C4.23 6.82 3 9.26 3 12c0 4.97 4.03 9 9 9s9-4.03 9-9c0-2.74-1.23-5.18-3.17-6.83z',
    public: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z',
    push_pin: 'M16 9V4h1c.55 0 1-.45 1-1s-.45-1-1-1H7c-.55 0-1 .45-1 1s.45 1 1 1h1v5c0 1.66-1.34 3-3 3v2h5.97v7l1 1 1-1v-7H19v-2c-1.66 0-3-1.34-3-3z',
    qr: 'M9.5 6.5v3h-3v-3h3M11 5H5v6h6V5zm-1.5 9.5v3h-3v-3h3M11 13H5v6h6v-6zm6.5-6.5v3h-3v-3h3M19 5h-6v6h6V5zm-6 8h1.5v1.5H13V13zm1.5 1.5H16V16h-1.5v-1.5zM16 13h1.5v1.5H16V13zm-3 3h1.5v1.5H13V16zm1.5 1.5H16V19h-1.5v-1.5zM16 16h1.5v1.5H16V16zm1.5-1.5H19V16h-1.5v-1.5zm0 3H19V19h-1.5v-1.5zM22 7h-2V4h-3V2h5v5zm0 15v-5h-2v3h-3v2h5zM2 22h5v-2H4v-3H2v5zM2 2v5h2V4h3V2H2z',
    refresh: 'M17.65 6.35C16.2 4.9 14.21 4 12 4c-4.42 0-7.99 3.58-7.99 8s3.57 8 7.99 8c3.73 0 6.84-2.55 7.73-6h-2.08c-.82 2.33-3.04 4-5.65 4-3.31 0-6-2.69-6-6s2.69-6 6-6c1.66 0 3.14.69 4.22 1.78L13 11h7V4l-2.35 2.35z',
    router: 'M20.2 5.9l.8-.8C19.6 3.7 17.8 3 16 3s-3.6.7-5 2.1l.8.8C13 4.8 14.5 4.2 16 4.2s3 .6 4.2 1.7zm-.9.8c-.9-.9-2.1-1.4-3.3-1.4s-2.4.5-3.3 1.4l.8.8c.7-.7 1.6-1 2.5-1 .9 0 1.8.3 2.5 1l.8-.8zM19 13h-2V9h-2v4H5c-1.1 0-2 .9-2 2v4c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2v-4c0-1.1-.9-2-2-2zM8 18H6v-2h2v2zm3.5 0h-2v-2h2v2zm3.5 0h-2v-2h2v2z',
    schedule: 'M11.99 2C6.47 2 2 6.48 2 12s4.47 10 9.99 10C17.52 22 22 17.52 22 12S17.52 2 11.99 2zM12 20c-4.42 0-8-3.58-8-8s3.58-8 8-8 8 3.58 8 8-3.58 8-8 8zm.5-13H11v6l5.25 3.15.75-1.23-4.5-2.67z',
    search: 'M15.5 14h-.79l-.28-.27C15.41 12.59 16 11.11 16 9.5 16 5.91 13.09 3 9.5 3S3 5.91 3 9.5 5.91 16 9.5 16c1.61 0 3.09-.59 4.23-1.57l.27.28v.79l5 4.99L20.49 19l-4.99-5zm-6 0C7.01 14 5 11.99 5 9.5S7.01 5 9.5 5 14 7.01 14 9.5 11.99 14 9.5 14z',
    send: 'M2.01 21L23 12 2.01 3 2 10l15 2-15 2z',
    settings: 'M19.14 12.94c.04-.3.06-.61.06-.94 0-.32-.02-.64-.07-.94l2.03-1.58c.18-.14.23-.41.12-.61l-1.92-3.32c-.12-.22-.37-.29-.59-.22l-2.39.96c-.5-.38-1.03-.7-1.62-.94l-.36-2.54c-.04-.24-.24-.41-.48-.41h-3.84c-.24 0-.43.17-.47.41l-.36 2.54c-.59.24-1.13.57-1.62.94l-2.39-.96c-.22-.08-.47 0-.59.22L2.74 8.87c-.12.21-.08.47.12.61l2.03 1.58c-.05.3-.09.63-.09.94s.02.64.07.94l-2.03 1.58c-.18.14-.23.41-.12.61l1.92 3.32c.12.22.37.29.59.22l2.39-.96c.5.38 1.03.7 1.62.94l.36 2.54c.05.24.24.41.48.41h3.84c.24 0 .44-.17.47-.41l.36-2.54c.59-.24 1.13-.56 1.62-.94l2.39.96c.22.08.47 0 .59-.22l1.92-3.32c.12-.22.07-.47-.12-.61l-2.01-1.58zM12 15.6c-1.98 0-3.6-1.62-3.6-3.6s1.62-3.6 3.6-3.6 3.6 1.62 3.6 3.6-1.62 3.6-3.6 3.6z',
    shield: 'M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm-2 16l-4-4 1.41-1.41L10 14.17l6.59-6.59L18 9l-8 8z',
    signal: 'M2 22h20V2z',
    stop: 'M6 6h12v12H6z',
    sweep: 'M15 16h4v2h-4zm0-8h7v2h-7zm0 4h6v2h-6zM3 18c0 1.1.9 2 2 2h6c1.1 0 2-.9 2-2V8H3v10zM14 5h-3l-1-1H6L5 5H2v2h12z',
    task_alt: 'M22 5.18L10.59 16.6l-4.24-4.24 1.41-1.41 2.83 2.83 10-10L22 5.18zm-2.21 5.04c.13.57.21 1.17.21 1.78 0 4.42-3.58 8-8 8s-8-3.58-8-8 3.58-8 8-8c1.58 0 3.04.46 4.28 1.25l1.44-1.44C16.1 2.67 14.13 2 12 2 6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10c0-1.19-.22-2.33-.6-3.39l-1.61 1.61z',
    terminal: 'M20 4H4c-1.11 0-2 .9-2 2v12c0 1.1.89 2 2 2h16c1.1 0 2-.9 2-2V6c0-1.1-.89-2-2-2zm0 14H4V8h16v10zm-2-1h-6v-2h6v2zM7.5 17l-1.41-1.41L8.67 13l-2.59-2.59L7.5 9l4 4-4 4z',
    tune: 'M3 17v2h6v-2H3zM3 5v2h10V5H3zm10 16v-2h8v-2h-8v-2h-2v6h2zM7 9v2H3v2h4v2h2V9H7zm14 4v-2H11v2h10zm-6-4h2V7h4V5h-4V3h-2v6z',
    undo: 'M12.5 8c-2.65 0-5.05.99-6.9 2.6L2 7v9h9l-3.62-3.62c1.39-1.16 3.16-1.88 5.12-1.88 3.54 0 6.55 2.31 7.6 5.5l2.37-.78C21.08 11.03 17.15 8 12.5 8z',
    wifi: 'M1 9l2 2c4.97-4.97 13.03-4.97 18 0l2-2C16.93 2.93 7.08 2.93 1 9zm8 8l3 3 3-3c-1.65-1.66-4.34-1.66-6 0zm-4-4l2 2c2.76-2.76 7.24-2.76 10 0l2-2C15.14 9.14 8.87 9.14 5 13z',
    wifi_off: 'M23.64 7c-.45-.34-4.93-4-11.64-4-1.5 0-2.89.19-4.15.48L18.18 13.8 23.64 7zm-6.6 8.22L3.27 1.44 2 2.72l2.05 2.06C1.91 5.76.59 6.82.36 7l11.63 14.49.01.01.01-.01 3.9-4.86 3.32 3.32 1.27-1.27-3.46-3.46z',
  };
  const icon = (n, cls = '') => `<svg class="ic ${cls}" viewBox="0 0 24 24" aria-hidden="true"><path d="${P[n]}"/></svg>`;
  // The Persona mark: a four-point star inside a coral ring.
  const star = () => '<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 2c.6 5.2 4.8 9.4 10 10-5.2.6-9.4 4.8-10 10-.6-5.2-4.8-9.4-10-10 5.2-.6 9.4-4.8 10-10z"/></svg>';

  /* ---------- building blocks ---------- */

  const appbar = ({ back, title, sub, dot, actions = [] }) => `
    <div class="appbar">
      ${back ? `<button class="iconbtn" data-go="${back}" aria-label="返回">${icon('arrow_back')}</button>` : '<span class="appbar-pad"></span>'}
      <div class="title ${sub ? 'small' : ''}">
        <div class="tl1">${dot ? `<span class="dot ${dot}"></span>` : ''}<b>${title}</b></div>
        ${sub ? `<small>${sub}</small>` : ''}
      </div>
      ${actions.map(a => `<button class="iconbtn" ${a.go ? `data-go="${a.go}"` : ''} aria-label="${a.label}">${icon(a.icon)}</button>`).join('')}
    </div>`;

  const TABS = [
    { id: 'persona', label: 'Persona', icon: 'auto_awesome' },
    { id: 'agents', label: 'Agents', icon: 'terminal', count: 3 },
    { id: 'inbox', label: '收件箱', icon: 'inbox', count: 3 },
    { id: 'settings', label: '设置', icon: 'settings' },
  ];
  const navbar = active => `<nav class="navbar">${TABS.map(t => `
    <div class="item ${t.id === active ? 'on' : ''}" data-go="${t.id}">
      <div class="pillwrap">${icon(t.icon)}${t.count ? `<span class="count">${t.count}</span>` : ''}</div>
      <span>${t.label}</span>
    </div>`).join('')}</nav>`;

  const btn = (label, kind = 'tonal small', attrs = '') => `<button class="btn ${kind}" ${attrs}>${label}</button>`;
  const okMark = `<span class="okmark">${icon('check')}</span>`;
  const li = ({ ic, cls = '', t, s = '', tail = '', go = '' }) => `
    <div class="li" ${go ? `data-go="${go}"` : ''}>
      ${ic ? `<span class="lic ${cls}">${icon(ic)}</span>` : ''}
      <div class="lb"><div class="lt">${t}</div>${s ? `<div class="ls">${s}</div>` : ''}</div>
      ${tail ? `<div class="tail">${tail}</div>` : ''}
    </div>`;

  const stp = (state, title, when = '', desc = '') => `
    <div class="stp ${state}">
      <div class="bullet">${state === 'done' ? icon('check') : state === 'active' ? '<span class="spinner neutral"></span>' : state === 'fail' ? icon('close', 'xs') : ''}</div>
      <div class="sbody"><div class="stt">${title}${when ? `<span class="when">${when}</span>` : ''}</div>${desc ? `<div class="sd">${desc}</div>` : ''}</div>
    </div>`;

  const cands = rows => `<div class="cand">${rows.map(r => `
    <div class="cr"><span class="tag ${r.tag || ''}">${r.kind}</span><span class="cres ${r.cls}">${r.res}</span><span class="addr mono">${r.addr}</span></div>`).join('')}</div>`;

  // A made-up QR code: random modules plus the three finder squares.
  const fakeQr = () => {
    const n = 25;
    let seed = 11, d = '';
    const rnd = () => ((seed = (seed * 1103515245 + 12345) & 0x7fffffff) / 0x7fffffff);
    const finder = (x, y) => (x < 8 && y < 8) || (x >= n - 8 && y < 8) || (x < 8 && y >= n - 8);
    for (let y = 0; y < n; y++) for (let x = 0; x < n; x++) if (!finder(x, y) && rnd() < 0.47) d += `M${x} ${y}h1v1h-1z`;
    const sq = (x, y) => `M${x} ${y}h7v7h-7zM${x + 1} ${y + 1}v5h5v-5zM${x + 2} ${y + 2}h3v3h-3z`;
    d += sq(0, 0) + sq(n - 7, 0) + sq(0, n - 7);
    return `<svg viewBox="-2 -2 ${n + 4} ${n + 4}" aria-hidden="true"><rect x="-2" y="-2" width="${n + 4}" height="${n + 4}" rx="1" fill="#f4f4f1"/><path d="${d}" fill="#1b1c20" fill-rule="evenodd"/></svg>`;
  };

  const row = r => `
    <div class="row" ${r.go ? `data-go="${r.go}"` : ''} ${r.focus ? 'data-focus="top"' : ''}>
      <div class="lead"><span class="dot ${r.state}"></span></div>
      <div class="main">
        <div class="top"><span class="t ${r.strong ? 'strong' : ''}">${r.title}</span><span class="time">${r.time}</span></div>
        <div class="last">${r.last}</div>
        <div class="meta">${r.meta}</div>
      </div>
    </div>`;

  const spaceHead = (name, where, whereIcon, counts) => `
    <div class="space-head">
      <span class="name">${name}</span><span class="where">${icon(whereIcon, 'xs')}${where}</span>
      <span class="counts">${counts.map(([s, n]) => `<span><span class="dot ${s} sm"></span>${n}</span>`).join('')}</span>
    </div>`;

  const statusline = ({ kind, text, detail = '', go = '' }) => `
    <div class="statusline ${kind}" ${go ? `data-go="${go}"` : ''}>
      ${kind === 'working' ? '<span class="spinner"></span>' : kind === 'needs' ? '<span class="dot needs sm"></span>' : kind === 'pending' ? icon('pending', 's') : ''}
      <span class="st">${text}</span><span class="detail">${detail}</span>
      <button class="iconbtn" aria-label="终端">${icon('terminal', 's')}</button>
    </div>`;

  const composer = ({ placeholder, end = 'send' }) => `
    <div class="composer">
      <button class="roundbtn plus" aria-label="添加">${icon('add')}</button>
      <div class="field">${placeholder}</div>
      ${end === 'stop'
        ? `<button class="roundbtn stop" aria-label="中断">${icon('stop')}</button>`
        : end === 'mic'
          ? `<button class="roundbtn plus" aria-label="语音">${icon('mic')}</button>`
          : `<button class="roundbtn send" aria-label="发送">${icon('send')}</button>`}
    </div>`;

  const sessTop = ({ dot, title, sub, back = 'agents' }) => appbar({
    back, dot, title, sub,
    actions: [{ icon: 'terminal', label: '终端' }, { icon: 'more', label: '更多：搜索、文件、Git', go: 'search' }],
  });
  const FRONT = { title: '重构结算页的表单校验', sub: `${icon('desktop', 'xs')}工作站 · web-shop › 前端` };

  /* ---------- screens ---------- */

  const SCREENS = [];
  const add = s => SCREENS.push(s);

  // Onboarding

  add({
    id: 'welcome', group: '接入', title: '欢迎',
    notes: [
      '主路径是「扫码配对」；手动输入时地址和配对码分两个框，粘贴整条 <code>persona://pair?…</code> 会自动拆开（ux.md 3.7）。',
      '「先体验一下」进体验模式：App 里一个假的 hub，数据和 iOS 共用同一份（android.md 5.16）。',
      '最下面一行回答「没有 Google 服务能不能收通知」。',
    ],
    render: () => `
      <div class="scroll">
        <div class="hero">
          <div class="brandmark"><div class="ring">${star()}</div></div>
          <h1>Persona</h1>
          <p>在手机上看各台电脑上的 Agent，<br>需要你拍板时，第一时间知道。</p>
        </div>
        <div class="feat"><span class="fi">${icon('terminal')}</span><div><div class="ft">Agents</div><div class="fd">实时看过程，回答提问、审批计划、批准工具</div></div></div>
        <div class="feat"><span class="fi">${icon('inbox')}</span><div><div class="ft">收件箱</div><div class="fd">飞书和邮件合成一个列表，AI 先分拣、草拟回复</div></div></div>
        <div class="feat"><span class="fi">${icon('auto_awesome')}</span><div><div class="ft">Persona</div><div class="fd">能读你的知识库和手机数据的助手</div></div></div>
      </div>
      <div class="actions">
        <button class="btn filled block" data-go="scan">${icon('qr')}扫码配对</button>
        <button class="btn outlined block" data-go="connect">手动输入地址和配对码</button>
        <button class="btn text block" data-go="agents">没有 hub？先体验一下</button>
        <p class="foot">${icon('notifications', 'xs')}<span>没有 Google 服务也能用：通知走和 hub 的常驻连接</span></p>
      </div>`,
  });

  add({
    id: 'scan', group: '接入', title: '扫码', chrome: 'dark',
    notes: [
      '二维码由 hub 机器上的 <code>personad hub pair</code> 打印：格式不变，token 换成 10 分钟内有效、只能用一次的临时配对码，另带 hub_id、公钥指纹、TLS 端口和一两个外部地址（ux.md 3.7，流程图 2）。',
      '现有 iOS 照样能扫这个二维码（只用里面的内网地址和 token），iOS 不用改。',
      '相机权限被拒时这一页换成手动输入，不反复弹授权。',
    ],
    render: () => `
      <div class="camera">
        <div class="cam-top">
          <button class="iconbtn" data-go="welcome" aria-label="关闭">${icon('close')}</button>
          <b>扫描配对二维码</b>
          <button class="iconbtn" aria-label="手电筒">${icon('flash')}</button>
        </div>
        <div class="qrframe"><div class="qrfake">${fakeQr()}</div><i></i><i></i><i></i><i></i><div class="scanline"></div></div>
        <div class="scanhint">在 hub 机器上运行 <code>personad hub pair</code>，<br>扫它打印的二维码；配对码 10 分钟内有效</div>
        <div class="scanbar"><div class="sbt">扫不了？可以手动输入地址和配对码</div><button class="btn tonal small" data-go="connect">手动输入</button></div>
      </div>`,
  });

  add({
    id: 'connect', group: '接入', title: '连接中（分段进度）',
    notes: [
      '分段进度：每段有时间戳，不显示假的百分比（ux.md 3.7）。',
      '几个候选地址错峰并发探测（Happy Eyeballs），顺序是公网 IPv6 → 域名 → 内网；先通过身份校验的赢，内网不在同一网络时跳过，不算失败（流程图 4）。',
      '「连得上」不够：公钥指纹和 hub_id 都要和二维码一致，对不上就停，不给「仍然继续」。旧的 IPv6 前缀可能已经分给了别人。',
      '时间戳和端口号（TLS 端口 18662）都是示例，TLS 端口在 hub 上可配。',
    ],
    render: () => `
      ${appbar({ back: 'scan', title: '连接 hub' })}
      <div class="scroll">
        <p class="pagetext lead">二维码里有 3 个地址：错开一点同时试，IPv6 先发；证书和 hub 身份都对得上的才算连上。</p>
        <div class="stepper">
          ${stp('done', '解析二维码', '10:24:01', '内网地址 · 2 个外部地址 · 公钥指纹 · 临时配对码')}
          ${stp('done', '探测地址', '10:24:02', `公网 IPv6 最先通过校验${cands([
            { kind: '公网 IPv6', tag: 'unread', res: '最先通过校验', cls: 'ok', addr: 'https://[2001:db8:12::7]:18662' },
            { kind: '域名', res: '也能连上', cls: 'ok', addr: 'https://hub.example.com:18662' },
            { kind: '内网', res: '不在同一网络，跳过', cls: 'skip', addr: 'https://192.0.2.10:18662' },
          ])}`)}
          ${stp('done', '校验 hub 身份', '10:24:02', '公钥指纹、hub_id 都和二维码里的一致')}
          ${stp('active', '换本机凭据', '', '用二维码里的临时配对码换这台手机自己的凭据，配对码随即作废')}
          ${stp('pending', '读机器列表')}
          ${stp('pending', '完成')}
        </div>
        <button class="linkrow">${icon('info', 's')}配对会保存什么</button>
      </div>
      <div class="actions inline"><button class="btn text" data-go="welcome">取消</button></div>`,
  });

  add({
    id: 'connect-fail', group: '接入', title: '连不上',
    notes: [
      '按原因说人话，并告诉用户下一步能做什么；不是一句「网络错误」（错误分类见 android.md 5.1）。',
      '公网 IPv6 最常见的坑是光猫、路由器默认拦入站：这里直接写出要放行哪个端口。',
      '「看诊断」进 设置 › 连接诊断，可以复制给别人看。',
    ],
    render: () => `
      ${appbar({ back: 'connect', title: '连接 hub' })}
      <div class="scroll">
        <div class="bigicon bad">${icon('wifi_off')}</div>
        <div class="pagetitle">公网 IPv6 连不上</div>
        <p class="pagetext">3 个地址都试过了。多半是光猫或路由器的 IPv6 防火墙挡住了从外面进来的连接。</p>
        <div class="pad16">${cands([
          { kind: '公网 IPv6', res: '超时', cls: 'bad', addr: 'https://[2001:db8:12::7]:18662' },
          { kind: '域名', res: '解析到同一个 IPv6，超时', cls: 'bad', addr: 'https://hub.example.com:18662' },
          { kind: '内网', res: '不在同一网络，没试', cls: 'skip', addr: 'https://192.0.2.10:18662' },
        ])}</div>
        <div class="todo">
          <div class="ti"><span class="n">1</span><span>在光猫或路由器的 IPv6 防火墙里放行 hub 的 TLS 端口 <code>18662</code> 入站，只放行这一个端口</span></div>
          <div class="ti"><span class="n">2</span><span>确认 hub 机器自己的防火墙也放行了 <code>18662</code>；在 hub 上运行 <code>personad hub doctor</code> 能看出卡在哪</span></div>
          <div class="ti"><span class="n">3</span><span>或者先连到和 hub 同一个 Wi-Fi，用内网地址配对</span></div>
        </div>
      </div>
      <div class="actions inline">
        <button class="btn text" data-go="diag">看诊断</button>
        <button class="btn outlined">手动输入</button>
        <button class="btn filled" data-go="connect">${icon('refresh')}重试</button>
      </div>`,
  });

  add({
    id: 'ready', group: '接入', title: '准备清单（国产系统）',
    notes: [
      '按厂商给保活引导（android.md 5.18）；每项都能跳过，并写清跳过会少什么（ux.md 3.7 准备清单）。',
      '已经满足的项直接打勾，不让用户再点一遍。',
      '「锁屏清理」是国产系统特有的坑：一键清理会把常驻连接一起杀掉。',
      '没有一家国产系统提供查自启动、查后台白名单的接口：查不了的项用户点「已完成」，再靠「锁屏测试」兜底；之后诊断页显示 hub 记录的断开时段（android.md 5.9、流程图 14）。',
    ],
    render: () => `
      <div class="scroll">
        <div class="bigicon ok">${icon('check_circle')}</div>
        <div class="pagetitle">已连上 hub</div>
        <p class="pagetext">这台手机是澎湃 OS。下面几项决定锁屏以后还能不能及时收到「需要你」，可以先跳过，之后在 设置 › 通知 里补。</p>
        <div class="card list">
          ${li({ ic: 'notifications', cls: 'ok', t: '通知', s: '横幅、锁屏都已允许', tail: okMark })}
          ${li({ ic: 'battery', cls: 'warn', t: '电池「无限制」', s: '不放开的话，锁屏一阵子后连接会被系统断开，「需要你」会晚到', tail: btn('去设置') })}
          ${li({ ic: 'power', cls: 'warn', t: '自启动', s: '不开的话，手机重启后要先打开一次 Persona', tail: btn('去设置') })}
          ${li({ ic: 'sweep', t: '锁屏清理', s: '在最近任务里锁住 Persona，一键清理时就不会被关掉', tail: btn('怎么做', 'text small') })}
          ${li({ ic: 'notifications', t: '锁屏测试', s: '锁屏 30 秒后 hub 发一条测试通知，看响不响、亮不亮屏', tail: btn('开始') })}
          ${li({ ic: 'wifi', cls: 'ok', t: '后台连接', s: '已连接 · 公网 IPv6', tail: okMark })}
          ${li({ ic: 'dns', cls: 'ok', t: '电脑', s: '3 台在线，版本一致', tail: okMark })}
        </div>
      </div>
      <div class="actions inline"><button class="btn text" data-go="agents">以后再说</button><button class="btn filled" data-go="agents">完成</button></div>`,
  });

  // Agents

  const agentsList = (scrolled = false, certChanged = false) => `
      <div class="appbar">
        <button class="iconbtn" aria-label="分组和范围">${icon('menu')}</button>
        <div class="title">${certChanged
          ? `<div class="conn-title">${icon('error', 's')}<b>hub 的证书变了</b></div>`
          : '<div class="tl1"><b>Agents</b></div>'}</div>
        <button class="iconbtn" aria-label="搜索">${icon('search')}</button>
        <button class="iconbtn" aria-label="新建会话">${icon('add')}</button>
      </div>
      <div class="chips">
        <button class="chip on" data-chip>全部 <span class="n">9</span></button>
        <button class="chip" data-chip><span class="dot needs sm"></span>需要你 <span class="n">3</span></button>
        <button class="chip" data-chip><span class="dot unread sm"></span>未读 <span class="n">1</span></button>
        <button class="chip" data-chip><span class="dot work sm"></span>工作中 <span class="n">2</span></button>
      </div>
      <div class="scroll">
        ${certChanged ? `<div class="notice caution">
          <div class="nt2">${icon('shield', 's')}<b>不会自动重连</b></div>
          <p>hub 的证书和配对时不一样：可能不是你的 hub，也可能 hub 换了证书。确认是自己换的，就重新扫码配对。</p>
          <div class="na"><button class="btn text small" data-go="diag">连接诊断</button><button class="btn tonal small" data-go="scan">重新扫码</button></div>
        </div>
        <div class="cached">${icon('history', 'xs')}显示的是本机缓存 · 3 分钟前</div>` : ''}
        <div class="sec">需要你处理 · 3</div>
        <div class="card">
          <div class="req" data-go="perm">
            <div class="head"><span class="kind">${icon('terminal', 's')}</span><span class="what">允许运行命令？</span><span class="left">${icon('schedule', 'xs')}01:12</span></div>
            <div class="sub"><code>npm run e2e -- --project=checkout</code></div>
            <div class="sub">web-shop › 前端 · 工作站<span class="tag">子 Agent · reviewer</span></div>
            <div class="acts"><button class="btn outlined small" data-toast="已拒绝">拒绝</button><button class="btn filled small" data-toast="已回传你的选择">允许</button></div>
          </div>
          <div class="req" data-go="plan">
            <div class="head"><span class="kind">${icon('assignment', 's')}</span><span class="what">批准计划：拆分订单服务的仓储层</span>${icon('chevron_right', 's')}</div>
            <div class="sub">data-pipeline › api · 笔记本 · 4 步</div>
          </div>
          <div class="req" data-go="question">
            <div class="head"><span class="kind">${icon('help', 's')}</span><span class="what">迁移脚本要不要保留旧表？</span></div>
            <div class="sub">data-pipeline › 迁移 · 笔记本</div>
            <div class="acts"><button class="chip" data-toast="已回传你的选择">保留 30 天<span class="tag ai">推荐</span></button><button class="chip" data-toast="已回传你的选择">直接删除</button><button class="chip" data-toast="已回传你的选择">先备份再删</button></div>
          </div>
        </div>
        ${spaceHead('web-shop', '工作站', 'desktop', [['needs', 1], ['work', 1], ['unread', 1]])}
        ${row({ state: 'needs', strong: true, title: '重构结算页的表单校验', time: '刚刚', go: 'chat', last: '用例改好了，要跑一遍结账流程的 e2e，等你允许运行命令。', meta: '前端 · <span class="needs">需要你</span> · 允许运行 Bash' })}
        ${row({ state: 'work', title: '补齐支付回调的日志', focus: scrolled, time: '2 分钟', last: '正在给退款回调加结构化日志，顺手把重复的字段收拢了…', meta: `后端 · 工作中 · ${icon('layers', 'xs')}后台 2` })}
        ${row({ state: 'unread', strong: true, title: '整理错误码文档', time: '10:02', last: '38 个错误码按模块整理成了表格，放在 docs/errors.md。', meta: '<span class="draft">草稿 · 先把 4xx 的说明改成…</span> · 文档' })}
        ${spaceHead('data-pipeline', '笔记本', 'laptop', [['needs', 2], ['work', 1]])}
        ${row({ state: 'needs', strong: true, title: '拆分订单服务的仓储层', time: '5 分钟', go: 'plan', last: '计划写好了：分 4 步，先抽接口，再迁移调用方。', meta: 'api · <span class="needs">需要你</span> · 批准计划' })}
        ${row({ state: 'needs', strong: true, title: '清理旧的订单表', time: '8 分钟', go: 'question', last: '迁移脚本写好了，有两个问题要你定。', meta: '迁移 · <span class="needs">需要你</span> · 回答提问' })}
        ${row({ state: 'work', title: '给订单接口补集成测试', time: '1 分钟', last: '跑到第 9 个用例，剩下 3 个要连测试库…', meta: `<span class="pend">${icon('pending', 'xs')}待确认</span> · 测试 · Codex · 工作中` })}
        ${row({ state: 'idle', title: '排查夜间任务失败', time: '昨天', last: '原因是时区：定时任务按 UTC 跑，日期边界错了一天。', meta: '调度 · 已看' })}
        <div class="fold-row">${icon('expand_more', 's')}空闲的空间 · 2</div>
      </div>
      ${navbar('agents')}`;

  add({
    id: 'agents', group: 'Agents', title: 'Agents 列表', nav: true,
    notes: [
      '快速筛选带计数；「需要你处理」升级成跨会话的请求收件箱：倒计时、来源（子 Agent / MCP / 终端）都在行上（ux.md 3.1）。',
      '单选且不超过 4 个选项的提问、授权可以在列表里直接处理；其他点开是完整交互页。',
      '行的元信息最前面放「草稿 · …」和「⏱ 待确认」。',
      '状态色和 iOS 同一张表（StatusPalette）：红 需要你、橙 工作中、绿 完成未看、灰 已看。',
    ],
    render: () => agentsList(),
  });

  add({
    id: 'agents-more', group: 'Agents', title: '往下翻：草稿与待确认', nav: true,
    notes: [
      '行的元信息最前面：有草稿时橙色「草稿 · 前 20 字」，有结果待确认的操作时「⏱ 待确认」（ux.md 3.1）。',
      '空间标题右边是这个空间各状态的数量；没有活动的空间折进「空闲的空间」。',
      '同一状态内按进入这个状态的时间排，列表刷新时行不跳。',
    ],
    render: () => agentsList(true),
  });

  add({
    id: 'conn', group: 'Agents', title: '连接要你处理（证书变了）', nav: true,
    notes: [
      '连接状态有四种平常的（在线、等待网络、连接中…、更新中…），只在标题位置显示，不弹横幅（ux.md 5.5）。',
      'Android 多三种要你处理的（需要本地网络权限、hub 的证书变了、配对已失效）：橙色，不自动重试（流程图 5）。',
      '证书变了可能是换了 hub，也可能是中间人：只给「重新扫码」和「连接诊断」，不给「仍然信任」。',
      '列表照常可看，标明是本机缓存。',
    ],
    render: () => agentsList(false, true),
  });

  // Conversation

  const chatBody = (tail = '') => `
    <div class="chat">
      <div class="datesep">今天 10:12</div>
      <div class="u-wrap"><div class="u-bubble">结算页的表单校验太乱了，抽成一个 hook，顺便把错误提示统一一下</div></div>
      <div class="workline" data-go="chat-expanded">${icon('expand_more')}<span>工具 5 · 思考 1 · <span class="fail">1 项失败</span> · 4 分 17 秒</span></div>
      <div class="md">
        <p>改好了，主要动了三处：</p>
        <table>
          <tr><th>文件</th><th>改动</th></tr>
          <tr><td><code>useCheckoutForm.ts</code></td><td>新增，校验和错误状态都在这里</td></tr>
          <tr><td><code>CheckoutForm.tsx</code></td><td>改用 hook，删掉 140 行</td></tr>
          <tr><td><code>errors.ts</code></td><td>错误提示统一成一张表</td></tr>
        </table>
        <div class="codeblock"><div class="ch"><span>useCheckoutForm.ts</span>${icon('copy', 'xs')}</div><pre class="mono"><span class="tk-k">export function</span> <span class="tk-f">useCheckoutForm</span>(rules) {
  <span class="tk-k">const</span> [errors, setErrors] = <span class="tk-f">useState</span>({})
  <span class="tk-k">const</span> validate = (v) =&gt;
    <span class="tk-f">setErrors</span>(<span class="tk-f">collect</span>(rules, v))
  <span class="tk-k">return</span> { errors, validate }
}</pre></div>
        <p>e2e 有一项失败：用例里的邮编还是 5 位，新规则要 6 位。要不要先改用例再重跑？</p>
      </div>
      <div class="u-wrap"><div class="u-bubble">先改用例，再重跑 e2e</div></div>
      ${tail}
    </div>`;

  add({
    id: 'chat', group: '对话', title: '对话页', end: true,
    notes: [
      '顶栏只放身份：状态圆点 + 标题 + 机器 · 空间 › tab。会话现在怎样，只在输入区上方的状态行里说（ux.md 3.2、5.1）。',
      '工作行常显「1 项失败」，不用展开就知道出过错（ux.md 3.3）。',
      '排队、循环、后台合成一行小标签；顶上「任务进度 · 3/7」只在有任务记录时出现。',
      '工作中时右下是中断；这时打的字会排队发送。',
    ],
    render: () => `
      ${sessTop({ dot: 'work', ...FRONT })}
      <div class="band" data-go="tasks">${icon('task_alt', 's')}<span><b>任务进度</b> · 3/7</span><span class="grow"></span>${icon('chevron_right', 's')}</div>
      <div class="scroll" data-end>${chatBody(`<div class="workline running"><span class="spinner"></span><span>工具 14 · 思考 2</span></div>`)}</div>
      <div class="bottomzone">
        ${statusline({ kind: 'working', text: '正在工作', detail: '3 分 12 秒 · 工具 14' })}
        <div class="strips"><button class="chip">${icon('list')}排队 2</button><button class="chip">${icon('loop')}循环</button><button class="chip">${icon('layers')}后台 3</button></div>
        ${composer({ placeholder: '补充说明（会排队发送）', end: 'stop' })}
      </div>`,
  });

  const expandedSteps = (hit = false) => `
    <div class="steps">
      <div class="step"><span class="si">${icon('description')}</span><div class="sb"><div class="sv"><b>读取</b> <code>CheckoutForm.tsx</code></div><div class="sdesc">312 行</div></div></div>
      <div class="step"><span class="si">${icon('search')}</span><div class="sb"><div class="sv"><b>搜索</b> <code>validate</code></div><div class="sdesc">7 处 · 4 个文件</div></div></div>
      <div class="step"><span class="si">${icon('edit')}</span><div class="sb"><div class="sv"><b>新建</b> <code>useCheckoutForm.ts</code> <span class="diffhead"><span class="p">+38</span></span></div></div></div>
      <div class="step"><span class="si">${icon('edit')}</span><div class="sb"><div class="sv"><b>编辑</b> <code>CheckoutForm.tsx</code> <span class="diffhead"><span class="p">+9</span><span class="m">−148</span></span></div>
        <div class="diffbox mono"><div class="dl del"><span class="s">−</span>const [zip, setZip] = useState('')</div><div class="dl del"><span class="s">−</span>const [tel, setTel] = useState('')</div><div class="dl add"><span class="s">+</span>const form = useCheckoutForm(rules)</div></div></div></div>
      <div class="step ${hit ? 'hit' : ''}" ${hit ? 'data-focus' : ''}><span class="si bad">${icon('terminal')}</span><div class="sb"><div class="sv"><b>运行</b> <code>npm run ${hit ? '<span class="mark">e2e</span>' : 'e2e'}</code></div><div class="err">${icon('error', 'xs')}出错了 · 退出码 1</div>
        <div class="out mono">✗ checkout › 邮编格式
  期望 6 位数字，收到 "12345"</div></div></div>
      <div class="step"><span class="si">${icon('lightbulb')}</span><div class="sb"><div class="sv"><b>思考</b></div><div class="sdesc">新规则是对的，失败是因为用例还按 5 位写：该改用例，不该放宽规则。</div></div></div>
      <div class="step end" data-go="chat">${icon('expand_less', 's')}收起 6 步</div>
    </div>`;

  add({
    id: 'chat-expanded', group: '对话', title: '工作过程展开',
    notes: [
      '每一步是独立的懒加载行；失败的那步就地显示错误和输出摘要，不用再点。',
      'diff 用和 iOS 一样的增删色；展开后最下面是「收起 6 步」，滚出顶部时吸附在顶上（P2）。',
    ],
    render: () => `
      ${sessTop({ dot: 'work', ...FRONT })}
      <div class="scroll"><div class="chat">
        <div class="u-wrap"><div class="u-bubble">结算页的表单校验太乱了，抽成一个 hook，顺便把错误提示统一一下</div></div>
        <div class="workline open" data-go="chat">${icon('expand_less')}<span>工具 5 · 思考 1 · <span class="fail">1 项失败</span> · 4 分 17 秒</span></div>
        ${expandedSteps()}
        <div class="md"><p>改好了，主要动了三处：</p></div>
      </div></div>
      <div class="bottomzone">
        ${statusline({ kind: 'working', text: '正在工作', detail: '3 分 12 秒 · 工具 14' })}
        ${composer({ placeholder: '补充说明（会排队发送）', end: 'stop' })}
      </div>`,
  });

  const result = (kind, text, mono = false) => `
    <div class="res" data-go="search-hit"><div class="rk">${kind}</div><div class="rx ${mono ? 'mono' : ''}">${text}</div></div>`;
  const m = s => `<span class="mark">${s}</span>`;

  add({
    id: 'search', group: '对话', title: '会话内搜索',
    notes: [
      '⋯ › 搜索。范围是已加载的正文、思考、工具名、工具输入 / 输出和系统事件，按类别分组（ux.md 3.3）。',
      '写明「已加载 6 轮」：更早的部分要点底部的按钮继续加载、继续搜，不假装搜了全部。',
    ],
    render: () => `
      <div class="searchbar">
        <button class="iconbtn" data-go="chat" aria-label="返回">${icon('arrow_back')}</button>
        <div class="q">e2e<span class="caret"></span></div>
        <button class="iconbtn" aria-label="清空">${icon('close')}</button>
      </div>
      <div class="searchmeta">8 条命中 · 已加载 6 轮</div>
      <div class="scroll">
        <div class="sec">正文 · 3</div>
        ${result('Claude · 10:16', `${m('e2e')} 有一项失败：用例里的邮编还是 5 位，新规则要 6 位。`)}
        ${result('你 · 10:17', `先改用例，再重跑 ${m('e2e')}`)}
        ${result('Claude · 昨天 18:40', `跑 ${m('e2e')} 之前要先起假的支付网关，命令写进 README 了。`)}
        <div class="sec">工具输入 · 2</div>
        ${result('运行 · 10:15', `npm run ${m('e2e')} -- --project=checkout`, true)}
        ${result('编辑 · 10:18', `${m('e2e')}/checkout.spec.ts`, true)}
        <div class="sec">工具输出 · 2</div>
        ${result(`运行结果 · 10:15 · <span class="fail">失败</span>`, `✗ 1 failed · ${m('e2e')}/checkout.spec.ts:42`, true)}
        ${result('运行结果 · 昨天 18:52', `✓ 23 passed · ${m('e2e')}/cart.spec.ts`, true)}
        <div class="sec">系统事件 · 1</div>
        ${result('后台任务 · 09:58', `${m('e2e')} 夜间回归已结束`)}
        <div class="loadmore"><button class="btn outlined">${icon('history')}加载更早的记录并继续搜</button></div>
      </div>`,
  });

  add({
    id: 'search-hit', group: '对话', title: '跳到命中',
    notes: [
      '点命中后：关掉搜索、展开那一轮、滚到那里描边 3 秒（ux.md 3.3）。',
      '顶部的「返回搜索前位置」（ReturnChip）把你带回原来读到的地方。',
    ],
    render: () => `
      ${sessTop({ dot: 'work', ...FRONT })}
      <button class="returnchip" data-go="chat">${icon('undo')}返回搜索前位置</button>
      <div class="scroll"><div class="chat">
        <div class="u-wrap"><div class="u-bubble">结算页的表单校验太乱了，抽成一个 hook，顺便把错误提示统一一下</div></div>
        <div class="workline open">${icon('expand_less')}<span>工具 5 · 思考 1 · <span class="fail">1 项失败</span> · 4 分 17 秒</span></div>
        ${expandedSteps(true)}
      </div></div>
      <div class="bottomzone">
        ${statusline({ kind: 'working', text: '正在工作', detail: '3 分 12 秒 · 工具 14' })}
        ${composer({ placeholder: '补充说明（会排队发送）', end: 'stop' })}
      </div>`,
  });

  const verifyChat = () => `
      ${sessTop({ dot: 'work', ...FRONT })}
      <div class="scroll" data-end>${chatBody(`<div class="workline">${icon('expand_more')}<span>工具 3 · 1 分 10 秒</span></div>
        <div class="u-wrap col"><div class="u-bubble pending">顺便把 4xx 的错误提示也改成新口径</div><div class="pend-label">${icon('pending', 'xs')}结果待确认</div></div>`)}</div>
      <div class="bottomzone">
        ${statusline({ kind: 'pending', text: '上次操作结果待确认', go: 'verify-sheet' })}
        ${composer({ placeholder: '输入消息', end: 'stop' })}
      </div>`;

  add({
    id: 'verify', group: '对话', title: '结果待确认',
    notes: [
      '发出去没回音时，气泡停在「结果待确认」：虚线描边加时钟，不画成红色错误，也不悄悄重发（ux.md 1、5.2、5.6）。',
      '状态行说「上次操作结果待确认」，点它打开会话状态浮层；Agents 列表那一行带「⏱ 待确认」。',
    ],
    render: verifyChat,
  });

  add({
    id: 'verify-sheet', group: '对话', title: '会话状态浮层',
    notes: [
      '新 hub 用同一个操作 id 自动核对，这里只显示进度（ux.md 3.3）。',
      '核对不出来才交给你：确认已送达，或确认没送达、把文字放回输入框。没把握就不重发，免得 Claude 收到两遍。',
    ],
    render: verifyChat,
    layer: () => `
      <div class="overlay" data-go="verify"></div>
      <div class="sheet">
        <div class="handle"></div>
        <div class="sh-title">会话状态</div>
        <div class="vbox">
          <div class="vq">「顺便把 4xx 的错误提示也改成新口径」</div>
          <div class="vrow"><span class="spinner caution"></span><span><b>正在核对</b> · 在对话记录里找这条消息</span></div>
          <div class="vsteps">
            <div>${icon('check', 'xs')}<span>10:31:05 交给了 hub</span></div>
            <div>${icon('help', 'xs')}<span>10:31:06 hub 转给电脑时连接断了，不知道字打完没有</span></div>
            <div>${icon('history', 'xs')}<span>核对了 40 秒，记录里还没出现</span></div>
          </div>
        </div>
        <p class="vnote">没把握就不重发：重发可能让 Claude 收到两遍。</p>
        <div class="sh-foot"><button class="btn text" data-toast="已标记为送达">确认已送达</button><button class="btn tonal" data-toast="文字已放回输入框">确认没送达，保留输入</button></div>
      </div>`,
  });

  const task = (state, t, d = '', link = false) => `
    <div class="task is-${state}">
      <span class="tstate is-${state}">${state === 'done' ? icon('check') : ''}</span>
      <div class="tb"><div class="tt">${t}</div>${d ? `<div class="td">${d}</div>` : ''}${link ? `<div class="tl" data-go="search-hit">查看相关记录 ›</div>` : ''}</div>
    </div>`;

  add({
    id: 'tasks', group: '对话', title: '任务进度',
    notes: [
      '从 TodoWrite / TaskCreate / TaskUpdate 的结果按顺序重建任务表（ux.md 3.3）。',
      '页脚明说「根据已加载的记录整理」：这是推导出来的视图，不假装是电脑上此刻的状态。',
    ],
    render: () => `
      ${appbar({ back: 'chat', title: '任务进度', sub: '重构结算页的表单校验' })}
      <div class="scroll">
        <div class="taskhead"><b>3 / 7</b><span>已完成</span></div>
        <div class="taskbar"><i></i></div>
        ${task('done', '梳理现有的校验规则', 'CheckoutForm.tsx 里一共 11 条', true)}
        ${task('done', '抽出 useCheckoutForm')}
        ${task('done', '错误提示统一成一张表')}
        ${task('doing', '修 e2e 里失败的用例', '邮编改成 6 位', true)}
        ${task('todo', '重跑 e2e')}
        ${task('todo', '给 hook 补单元测试')}
        ${task('todo', '更新组件文档')}
        <div class="footnote">${icon('info', 'xs')}<span>根据已加载的记录整理，可能和电脑上此刻不同</span></div>
      </div>`,
  });

  // Interaction

  const needsLine = (what, detail = '4 分 02 秒') => statusline({ kind: 'needs', text: `等你处理：${what}`, detail });
  const permTail = `
    <div class="workline">${icon('expand_more')}<span>工具 2 · 1 分 05 秒</span></div>
    <div class="md"><p>用例改好了。让 reviewer 子 Agent 重跑一遍结账流程的 e2e。</p></div>`;

  const permPanel = (reason = '') => `
    <div class="ixpanel ${reason ? 'has-reason' : ''}">
      <div class="ixh"><span class="kind">${icon('terminal', 's')}</span><span class="t">允许运行命令？</span><span class="left">${icon('schedule', 'xs')}01:12</span><button class="iconbtn sm" data-go="ix-full" aria-label="打开完整页">${icon('open_full', 's')}</button></div>
      <div class="src">Bash<span class="tag">来自子 Agent · reviewer</span></div>
      <div class="cmd mono">npm run e2e -- --project=checkout</div>
      <div class="why">跑一遍结账流程的端到端测试，确认邮编用例改好了</div>
      <textarea class="field-area" data-reason rows="1" placeholder="拒绝原因（可选，会交给 Claude）">${reason}</textarea>
      <div class="hint reason">${icon('info', 'xs')}要允许请先清空原因</div>
      <div class="ixacts">
        <button class="btn text small needs-empty push" data-toast="已回传你的选择">本会话总是允许</button>
        <button class="btn outlined" data-toast="${reason ? '已拒绝，原因已交给 Claude' : '已拒绝'}">拒绝</button>
        <button class="btn filled needs-empty" data-toast="已回传你的选择">允许</button>
      </div>
    </div>`;

  const permScreen = reason => () => `
    ${sessTop({ dot: 'needs', ...FRONT })}
    <div class="scroll" data-end>${chatBody(permTail)}</div>
    <div class="bottomzone">
      ${needsLine('允许运行 Bash')}
      ${permPanel(reason)}
      ${composer({ placeholder: '输入消息', end: 'stop' })}
    </div>`;

  add({
    id: 'perm', group: '交互', title: '授权（面板）',
    notes: [
      '交互面板固定在输入框上方，往上翻历史时也一直看得见（android.md 5.7）。',
      '倒计时、完整命令、来源（这里是子 Agent）都在面板上；右上角打开完整页（ux.md 3.4）。',
      '拒绝原因可选，会随拒绝交给 Claude；状态行同时说「等你处理：允许运行 Bash」。',
      '点「允许」只出「已回传你的选择」，不说「已执行」：执行结果看对话记录。',
    ],
    render: permScreen(''),
  });

  add({
    id: 'perm-reason', group: '交互', title: '授权：写了拒绝原因',
    notes: [
      '写了拒绝原因，「允许」和「本会话总是允许」就禁用，并提示「要允许请先清空原因」：不会出现写了原因却点了允许。',
      '在手机里的输入框清空试试，按钮会恢复。',
    ],
    render: permScreen('e2e 会连测试环境的支付网关，先别跑，只跑单元测试'),
  });

  const PLAN = { title: '拆分订单服务的仓储层', sub: `${icon('laptop', 'xs')}笔记本 · data-pipeline › api` };
  const planPanel = (feedback = '') => `
    <div class="ixpanel ${feedback ? 'has-feedback' : ''}">
      <div class="ixh"><span class="kind">${icon('assignment', 's')}</span><span class="t">批准计划？</span><button class="iconbtn sm" data-go="ix-full" aria-label="全屏看计划">${icon('open_full', 's')}</button></div>
      <div class="planprev">
        <h5>拆分订单服务的仓储层</h5>
        <ol>
          <li>抽出 <code>OrderRepository</code> 接口，现有实现挪进 <code>pg/</code></li>
          <li>3 处直接拼 SQL 的调用方改走接口</li>
          <li>给接口补一个内存实现，单测改用它</li>
          <li>删掉旧的 <code>dao</code> 包</li>
        </ol>
      </div>
      <textarea class="field-area" data-feedback rows="1" placeholder="意见（写了就只能打回）">${feedback}</textarea>
      <div class="hint feedback">${icon('info', 'xs')}要批准请先清空意见</div>
      <div class="ixacts">
        <button class="btn outlined" data-toast="已打回，意见已交给 Claude">打回</button>
        <button class="btn filled needs-empty" data-toast="已回传你的选择">批准，只自动接受编辑</button>
      </div>
      <div class="ixacts tight"><button class="btn text small needs-empty" data-toast="已回传你的选择">批准并自动执行</button></div>
      <div class="risk needs-empty">等于全部放行：之后的命令和编辑都不再问你</div>
    </div>`;
  const planScreen = feedback => () => `
    ${sessTop({ dot: 'needs', ...PLAN })}
    <div class="scroll" data-end><div class="chat">
      <div class="datesep">今天 10:05</div>
      <div class="u-wrap"><div class="u-bubble">订单服务的仓储层太乱了，先出个拆分计划给我看，别急着改</div></div>
      <div class="workline">${icon('expand_more')}<span>工具 9 · 思考 2 · 2 分 40 秒</span></div>
      <div class="md"><p>看了 <code>orders/</code> 下的 23 个文件：直接拼 SQL 的地方有 3 处，单测都连着真库。计划在下面，等你批准。</p></div>
    </div></div>
    <div class="bottomzone">
      ${needsLine('批准计划', '2 分 40 秒')}
      ${planPanel(feedback)}
      ${composer({ placeholder: '输入消息' })}
    </div>`;

  add({
    id: 'plan', group: '交互', title: '计划审批',
    notes: [
      '计划只露前几行，右上角全屏看全文。',
      '「批准并自动执行」等于全部放行，下面一行小字写出风险。通知、锁屏、快捷入口上只能「批准，只自动接受编辑」：在那些地方看不全计划（审批策略表，ux.md 3.4）。',
    ],
    render: planScreen(''),
  });

  add({
    id: 'plan-feedback', group: '交互', title: '计划：写了意见',
    notes: ['写了意见就只能「打回」，两个批准都禁用；清空意见才能批准。'],
    render: planScreen('第 4 步先别删 dao 包，留一个版本做回退'),
  });

  const MIG = { title: '清理旧的订单表', sub: `${icon('laptop', 'xs')}笔记本 · data-pipeline › 迁移` };
  add({
    id: 'question', group: '交互', title: '提问',
    notes: [
      '多题翻页；「推荐」用 AI 紫；最后一项「其他回答…」自己写（ux.md 3.4）。',
      '单题单选时点一下就提交，不用翻页。',
      '没提交的选择按请求 id 存在本机，关掉再开能恢复；请求被替换就丢掉。',
    ],
    render: () => `
      ${sessTop({ dot: 'needs', ...MIG })}
      <div class="scroll" data-end><div class="chat">
        <div class="datesep">今天 10:09</div>
        <div class="u-wrap"><div class="u-bubble">把旧的订单表清理掉，迁移脚本你来写</div></div>
        <div class="workline">${icon('expand_more')}<span>工具 6 · 1 分 52 秒</span></div>
        <div class="md"><p>迁移脚本写好了（<code>0042_drop_orders_v1.sql</code>），有两个问题要你定。</p></div>
      </div></div>
      <div class="bottomzone">
        ${needsLine('回答提问', '1 分 52 秒')}
        <div class="ixpanel">
          <div class="ixh"><span class="kind">${icon('help', 's')}</span><span class="t">提问 · 1/2</span><button class="iconbtn sm" data-go="ix-full" aria-label="打开完整页">${icon('open_full', 's')}</button></div>
          <div class="qtext">迁移脚本要不要保留旧表？</div>
          <div class="qopts">
            <div class="qopt sel" data-opt><span class="num">1</span><div><div class="qt">保留 30 天<span class="tag ai">推荐</span></div><div class="qd">改名成 orders_legacy，30 天后定时删掉</div></div></div>
            <div class="qopt" data-opt><span class="num">2</span><div><div class="qt">直接删除</div><div class="qd">迁移校验通过后立即删表</div></div></div>
            <div class="qopt" data-opt><span class="num">3</span><div><div class="qt">先备份再删</div><div class="qd">导出一份快照，然后删表</div></div></div>
            <div class="qopt other" data-opt><span class="num">${icon('edit', 'xs')}</span><div><div class="qt">其他回答…</div></div></div>
          </div>
          <div class="qnav"><button class="btn text is-disabled">上一题</button><span class="muted">1 / 2</span><button class="btn tonal">下一题</button></div>
        </div>
        ${composer({ placeholder: '输入消息' })}
      </div>`,
  });

  add({
    id: 'ix-full', group: '交互', title: '完整交互页（MCP 工具）',
    notes: [
      '完整页是全屏的 ModalBottomSheet；系统返回关闭，并按请求 id 保存没写完的原因（ux.md 9）。',
      'MCP 工具写出服务器名和配置来源，没给就写「未提供」，不从工具名去猜（ux.md 3.4）。',
      '「交给电脑上的终端」：不在手机上处理这件事。',
    ],
    render: permScreen(''),
    layer: () => `
      <div class="overlay" data-go="perm"></div>
      <div class="sheet full">
        <div class="handle"></div>
        <div class="sh-head"><span class="kind">${icon('extension', 's')}</span><div class="shh"><div class="sh-title">允许调用 MCP 工具？</div><div class="sh-sub"><span class="ell">web-shop › 前端 · 工作站</span><span class="left">${icon('schedule', 'xs')}00:58</span></div></div></div>
        <div class="scroll">
          <div class="kvs">
            <div class="kv"><span class="k2">工具</span><span class="v2"><code>create_pull_request</code></span></div>
            <div class="kv"><span class="k2">来自</span><span class="v2">MCP 服务器 github</span></div>
            <div class="kv"><span class="k2">配置来源</span><span class="v2">项目里的 <code>.mcp.json</code></span></div>
            <div class="kv"><span class="k2">发起</span><span class="v2">子 Agent · reviewer</span></div>
          </div>
          <div class="sec">参数</div>
          <div class="kvs">
            <div class="kv"><span class="k2 mono">title</span><span class="v2">结算页表单校验抽成 hook</span></div>
            <div class="kv"><span class="k2 mono">base</span><span class="v2 mono">main</span></div>
            <div class="kv"><span class="k2 mono">head</span><span class="v2 mono">refactor/checkout-form</span></div>
            <div class="kv"><span class="k2 mono">body</span><span class="v2">抽出 useCheckoutForm，错误提示统一成一张表；e2e 全部通过。</span></div>
            <div class="kv"><span class="k2 mono">draft</span><span class="v2 mono">false</span></div>
          </div>
          <div class="pad16"><textarea class="field-area" data-reason rows="2" placeholder="拒绝原因（可选，会交给 Claude）"></textarea><div class="hint reason">${icon('info', 'xs')}要允许请先清空原因</div></div>
          <button class="btn text handoff">${icon('terminal', 's')}交给电脑上的终端</button>
        </div>
        <div class="sh-foot"><button class="btn outlined" data-toast="已拒绝">拒绝</button><button class="btn filled needs-empty" data-toast="已回传你的选择">允许这一次</button></div>
      </div>`,
  });

  // Notifications

  const appIcon = `<span class="appi">${star()}</span>`;
  const fleet = (label = '已连接 hub') => `
    <div class="nh">${appIcon}<span>Persona · ${label}</span><span class="grow"></span>${icon('expand_less', 'xs')}</div>
    <div class="metrics"><div class="m crit"><b>3</b><span>需要你</span></div><div class="m"><b>2</b><span>工作中</span></div><div class="m"><b>1</b><span>完成未看</span></div></div>`;
  const shade = reply => () => `
    <div class="shade-head"><div><div class="big">10:24</div><div class="date">10月30日 周四</div></div><span class="grow"></span><button class="iconbtn" aria-label="设置">${icon('settings')}</button></div>
    <div class="qs"><div class="tile on">${icon('wifi')}<span>Wi-Fi</span></div><div class="tile">${icon('notifications_off')}<span>勿扰</span></div></div>
    <div class="scroll">
      <div class="notif" data-go="agents">
        ${fleet()}
        <div class="nb first">先处理：web-shop › 前端 · 允许运行 Bash</div>
      </div>
      <div class="notif" data-go="perm">
        <div class="nh">${appIcon}<span>Persona · 需要你 · 刚刚</span></div>
        <div class="nt">允许运行命令？</div>
        <div class="nb">web-shop › 前端 · 工作站</div>
        <div class="nb"><code>npm run e2e -- --project=checkout</code></div>
        ${reply
          ? `<div class="nreply"><div class="rfield">只跑单元测试，e2e 先别跑<span class="caret"></span></div><button class="rsend" data-toast="已拒绝，原因已交给 Claude" aria-label="发送">${icon('send', 's')}</button></div>
             <div class="rhint">拒绝，并把原因交给 Claude</div>`
          : `<div class="nacts"><button data-toast="已回传你的选择">允许</button><button data-toast="已拒绝">拒绝</button><button data-go="shade-reply">写原因</button></div>`}
        <div class="nmore">另外 2 件：批准计划 · 回答提问</div>
      </div>
      <div class="notif">
        <div class="nh">${appIcon}<span>Persona · 做完了 · 2 分钟前</span></div>
        <div class="nt">整理错误码文档</div>
        <div class="nb">工作站 · web-shop › 文档</div>
        <div class="nb">38 个错误码按模块整理成了表格，放在 docs/errors.md。</div>
      </div>
      <div class="shade-foot"><button>管理</button><button>全部清除</button></div>
    </div>`;

  add({
    id: 'shade', group: '通知', title: '通知栏',
    notes: [
      '前台服务那条常驻通知兼做 Fleet 汇总：三个数 + 先处理哪件；Android 17 用 MetricStyle，「需要你」是关键指标（android.md 5.10）。',
      '「允许 / 批准」在锁屏上点要先解锁，「拒绝 / 打回」不用；拒绝可以直接写原因（RemoteInput，android.md 5.8）。',
      '处理完的请求，通知会被撤回；「做完了」由 hub 判断，和 iPhone 的推送一致（hub-changes.md 3.9）。',
      '没有 Google 服务：这些通知都来自和 hub 的常驻连接。',
    ],
    backdrop: () => '<div class="wall"></div><div class="shade-bg"></div>',
    render: shade(false),
  });

  add({
    id: 'shade-reply', group: '通知', title: '通知里直接写原因',
    notes: ['点「写原因」在通知里直接输入，发出去就是「拒绝 + 原因」，不用打开 App。计划的「打回」同样可以写意见。'],
    backdrop: () => '<div class="wall"></div><div class="shade-bg"></div>',
    render: shade(true),
  });

  add({
    id: 'lock', group: '通知', title: '锁屏', chrome: 'dark',
    notes: [
      '锁屏只显示公开版本：「3 件事需要你」和三个数，内容解锁后才看得到；对应 iOS 的「锁屏显示详情」开关（android.md 5.10）。',
      '「需要你」始终走普通的高优先级通知，不依赖 Live Updates：厂商可能限制，用户也能一键关掉。',
    ],
    backdrop: () => '<div class="wall lock"></div>',
    render: () => `
      <div class="clock">${icon('lock', 's')}<div class="hm">10:24</div><div class="date">10月30日 周四</div></div>
      <div class="locknotes">
        <div class="notif">
          <div class="nh">${appIcon}<span>Persona · 刚刚</span></div>
          <div class="nt">3 件事需要你</div>
          <div class="nb">内容已隐藏，解锁后查看</div>
        </div>
        <div class="notif">${fleet()}</div>
      </div>
      <div class="lockfoot">上滑解锁</div>`,
  });

  // Persona

  const personaHome = () => `
    <div class="jtop">
      <button class="iconbtn" data-go="drawer" aria-label="对话列表">${icon('menu')}</button>
      <div class="who"><div class="pill"><span class="face">${star()}</span><b>Persona</b>${icon('expand_more', 'xs')}</div></div>
      <button class="iconbtn" aria-label="新对话">${icon('add_comment')}</button>
    </div>
    <div class="scroll">
      <div class="greet"><h2>上午好</h2><p>2 个 Agent 在工作，3 件事等你处理；收件箱里有 2 条 @你 的消息。</p></div>
      <div class="devs"><span class="devl">能访问</span>
        <span class="tag outline">${icon('desktop', 'xs')}工作站</span>
        <span class="tag outline">${icon('laptop', 'xs')}笔记本</span>
        <span class="tag outline">${icon('phone', 'xs')}这台手机</span>
      </div>
      <div class="sugg">
        <div class="sc">${icon('inbox')}<b>分拣收件箱</b><span>先看 @我 和要回复的</span></div>
        <div class="sc">${icon('terminal')}<b>Agent 进度</b><span>各台电脑上在做什么</span></div>
        <div class="sc">${icon('event')}<b>今天的日程</b><span>读手机日历</span></div>
        <div class="sc">${icon('alarm')}<b>5 点提醒我</b><span>到点在手机上跟进</span></div>
        <div class="sc">${icon('book')}<b>查知识库</b><span>人、群、项目</span></div>
        <div class="sc">${icon('description')}<b>总结一篇文档</b><span>粘贴链接就行</span></div>
      </div>
    </div>
    <div class="bottomzone">
      <div class="jchips"><button class="chip">${icon('lock', 'xs')}默认权限</button><button class="chip">${icon('lightbulb', 'xs')}思考：中</button></div>
      ${composer({ placeholder: '问 Persona…', end: 'mic' })}
    </div>
    ${navbar('persona')}`;

  add({
    id: 'persona', group: 'Persona', title: 'Persona 首页', nav: true,
    notes: [
      '结构和 iOS 一样：主对话、历史在左边的抽屉里（ModalNavigationDrawer），宽屏时是常驻的一列（ux.md 9）。',
      '首句发送和普通消息一样走统一的发送：断网时排队，不直接报错（ux.md 3.9）。',
    ],
    render: personaHome,
  });

  add({
    id: 'drawer', group: 'Persona', title: '对话抽屉', nav: true,
    notes: ['抽屉：主对话、定时任务、知识库，然后是置顶和最近的对话；系统返回或点空白关闭。'],
    render: personaHome,
    layer: () => `
      <div class="overlay" data-go="persona"></div>
      <div class="drawer">
        <div class="dtitle">Persona</div>
        <div class="dsearch">${icon('search', 's')}<span>搜索对话</span></div>
        <div class="di on" data-go="persona">${icon('chat', 's')}<span class="grow">主对话</span></div>
        <div class="di">${icon('alarm', 's')}<span class="grow">定时任务</span><small>3</small></div>
        <div class="di">${icon('book', 's')}<span class="grow">知识库</span></div>
        <div class="dh">置顶</div>
        <div class="di">${icon('push_pin', 's')}<span class="grow">周报素材</span></div>
        <div class="dh">最近</div>
        <div class="di">${icon('chat', 's')}<span class="grow">下周出差的行程</span><small>昨天</small></div>
        <div class="di">${icon('chat', 's')}<span class="grow">飞书文档的要点</span><small>周二</small></div>
        <div class="di">${icon('chat', 's')}<span class="grow">订单服务的错误码</span><small>周一</small></div>
        <div class="dfoot"><button class="btn filled">${icon('add_comment')}新对话</button><button class="iconbtn" data-go="settings" aria-label="设置">${icon('settings')}</button></div>
      </div>`,
  });

  const target = (ic, t, s, sel = false) => `
    <div class="li opt ${sel ? 'sel' : ''}" data-opt><span class="radio"></span><span class="lic">${icon(ic)}</span><div class="lb"><div class="lt">${t}</div><div class="ls">${s}</div></div></div>`;

  add({
    id: 'share', group: 'Persona', title: '分享到 Persona', chip: '需要你 3',
    notes: [
      'Android 独有、首版就做：任何 App 里的文字、链接、图片、文件「分享 → Persona」，选发给主对话还是某个 Agent（android.md 6.3）。',
      '默认作为 &lt;persona-context&gt; 附在消息里，不单独成一条。',
      '状态栏左上的「需要你 3」是 Live Update 的胶囊（Android 16+），在别的 App 里也看得到。',
    ],
    backdrop: () => `
      <div class="hostapp">
        <div class="hbar">${icon('lock', 'xs')}<div class="url">example.com/posts/form-hooks</div>${icon('more', 's')}</div>
        <article><h3>用 hook 收拢表单校验的 5 个理由</h3><p>表单一多，校验规则就散落在各个组件里：同一条邮编规则写了三遍，错误提示的口径也各不相同。</p><p>把规则和错误状态收进一个 hook，组件只管展示……</p></article>
      </div>`,
    render: () => '',
    layer: () => `
      <div class="overlay"></div>
      <div class="sheet">
        <div class="handle"></div>
        <div class="sh-title">发给 Persona</div>
        <div class="sharecard"><span class="thumb"></span><div class="lb"><div class="sct">用 hook 收拢表单校验的 5 个理由</div><div class="scu">example.com · 文章链接</div></div></div>
        <div class="pad16"><textarea class="field-area" rows="2" placeholder="补一句（可选）">参考这篇，看看我们的 useCheckoutForm 还缺什么</textarea></div>
        <div class="sec">发到</div>
        ${target('auto_awesome', '主对话', 'Persona', true)}
        ${target('terminal', 'web-shop › 前端', '工作站 · 需要你')}
        ${target('terminal', 'data-pipeline › api', '笔记本 · 需要你')}
        ${li({ t: '作为上下文附在消息里', s: '带上链接、标题和选中的文字', tail: '<span class="switch" data-switch></span>' })}
        <div class="sh-foot"><button class="btn text">取消</button><button class="btn filled" data-toast="已发给主对话">${icon('send', 's')}发送</button></div>
      </div>`,
  });

  // Inbox

  const irow = ({ av, avCls = '', src = '', title, time, summ, meta = '', go = '' }) => `
    <div class="row irow" ${go ? `data-go="${go}"` : ''}>
      <span class="avatar ${avCls}">${av}${src ? `<span class="src ${src}">${icon(src === 'lark' ? 'chat' : 'mail')}</span>` : ''}</span>
      <div class="main">
        <div class="top"><span class="t strong">${title}</span><span class="time">${time}</span></div>
        <div class="summ">${summ}</div>
        ${meta ? `<div class="meta">${meta}</div>` : ''}
      </div>
    </div>`;
  const aiMark = `<span class="ai">${icon('auto_awesome', 'xs inl')}</span>`;

  add({
    id: 'inbox', group: '收件箱', title: '收件箱', nav: true,
    notes: [
      '飞书和邮件一个列表，一行一件事；「等你的 Agent」并进「需要你」（android.md 5.15）。',
      'AI 草拟的回复点一下就发；发送走 outbox，带幂等 id（ux.md 3.6）。',
      '不做第二个飞书：没有群列表、通讯录这些页面。',
    ],
    render: () => `
      ${appbar({ title: '收件箱', actions: [{ icon: 'search', label: '搜索' }, { icon: 'more', label: '更多' }] })}
      <div class="chips">
        <button class="chip on" data-chip>全部</button>
        <button class="chip" data-chip>需要你 <span class="n">3</span></button>
        <button class="chip" data-chip>飞书</button>
        <button class="chip" data-chip>邮件</button>
        <button class="chip" data-chip>已处理</button>
      </div>
      <div class="scroll">
        <div class="sec">需要你 · 3</div>
        ${irow({ av: icon('terminal'), avCls: 'av-ix', title: 'web-shop › 前端', time: '刚刚', go: 'perm', summ: '等你允许运行命令：<code>npm run e2e</code>', meta: `<span class="tag needs">Agent</span><span class="left-s">${icon('schedule', 'xs')}还剩 01:12</span>` })}
        ${irow({ av: '王', avCls: 'av1', src: 'lark', title: '产品群', time: '10:18', go: 'lark', summ: '小王：下午的评审改到 4 点，<span class="at">@你</span> 那边能来吗？', meta: '<span class="tag">飞书</span><span class="tag needs">@我</span>' })}
        <div class="draftcard">
          <div class="dt">${icon('auto_awesome', 'xs')}AI 草拟的回复</div>
          <div class="dx">4 点可以，我会带上结算页的改动说明。</div>
          <div class="da"><button class="btn text small">修改</button><button class="btn tonal small" data-toast="已发送">${icon('send', 'xs')}发送</button></div>
        </div>
        ${irow({ av: '账', avCls: 'av3', src: 'mail', title: '10 月对账单', time: '09:40', summ: `${aiMark}对方要你周五前确认 3 笔差异`, meta: '<span class="tag">邮件</span><span class="ell">billing@example.com</span>' })}
        <div class="sec">知悉即可 · 4<span class="grow"></span><button class="act" data-toast="已全部标为看过">全部看过</button></div>
        ${irow({ av: '李', avCls: 'av4', src: 'lark', title: '前端组', time: '09:12', summ: '小李：周四的发版窗口挪到晚上 8 点' })}
        ${irow({ av: '构', avCls: 'av5', src: 'mail', title: '构建报告', time: '08:30', summ: `${aiMark}夜间构建通过，覆盖率比上周高一点` })}
        <div class="fold-row">${icon('expand_more', 's')}已处理 · 12</div>
      </div>
      ${navbar('inbox')}`,
  });

  add({
    id: 'lark', group: '收件箱', title: '飞书会话', end: true,
    notes: [
      '从收件箱打开时停在这件事的第一条，上面加分界线「这件事从这里开始」（ux.md 3.5）。',
      '视频、语音不在 App 里播：显示卡片，点开交给飞书。',
      '发出的消息按 clientId 幂等，确认只认 clientId 或服务端 id，不按文字比（ux.md 3.6）。',
    ],
    render: () => `
      ${appbar({ back: 'inbox', title: '产品群', sub: '飞书 · 群聊 · 12 人', actions: [{ icon: 'auto_awesome', label: '让 AI 总结' }, { icon: 'more', label: '更多' }] })}
      <div class="scroll" data-end><div class="lchat">
        <div class="datesep">今天</div>
        <div class="lmsg"><span class="avatar av4 sm">李</span><div class="lb"><div class="ln">小李 · 09:58</div><div class="lbub">结算页的改版下周一上线，手上的事情周五前收尾。</div></div></div>
        <div class="lmsg cont"><span class="avspace"></span><div class="lb"><div class="lbub">评审放在今天下午 3 点。</div></div></div>
        <div class="newline">这件事从这里开始</div>
        <div class="lmsg"><span class="avatar av1 sm">王</span><div class="lb"><div class="ln">小王 · 10:18</div><div class="lbub">下午的评审改到 4 点，<span class="at">@你</span> 那边能来吗？</div></div></div>
        <div class="lmsg cont"><span class="avspace"></span><div class="lb">
          <div class="videocard"><div class="vthumb"><span class="play">${icon('play')}</span><span class="dur">0:42</span></div><div class="vfoot"><span class="ell">评审演示录屏</span><button class="btn text small">${icon('open_new', 'xs')}在飞书中打开</button></div></div>
        </div></div>
        <div class="lmsg"><span class="avatar av2 sm">陈</span><div class="lb"><div class="ln">小陈 · 10:21</div><div class="lbub">我可以，4 点见</div><div class="reacts"><span>👍 2</span></div></div></div>
      </div></div>
      <div class="bottomzone">${composer({ placeholder: '回复 产品群' })}</div>`,
  });

  // Settings

  const chev = icon('chevron_right');
  add({
    id: 'settings', group: '设置', title: '设置', nav: true,
    notes: [
      '最上面一行就是连接状态，点进去是诊断。',
      '后台保活没做完时，这里一直提示还差几项。',
      '没有应用商店：新版本的 APK 放在 hub 上，经 TLS 下载、校验后交给系统安装器（流程图 15）。',
    ],
    render: () => `
      ${appbar({ title: '设置' })}
      <div class="scroll">
        <div class="card list">
          ${li({ ic: 'dns', cls: 'ok', t: 'hub', s: '已连接 · 公网 IPv6 · 3 台电脑在线', tail: chev, go: 'diag' })}
          ${li({ ic: 'refresh', cls: 'ok', t: '有新版本 0.2.0', s: '从 hub 下载，系统安装器确认后覆盖安装', tail: btn('更新', 'tonal small', 'data-toast="正在经 TLS 下载，校验后交给系统安装器"') })}
        </div>
        <div class="sec">这台手机</div>
        <div class="card list">
          ${li({ ic: 'notifications', t: '通知', s: '需要你、做完了、锁屏常驻通知', tail: chev, go: 'settings-notify' })}
          ${li({ ic: 'battery', cls: 'warn', t: '后台保活', s: '澎湃 OS · 还差 1 项', tail: `<span class="tag caution">1</span>${chev}`, go: 'ready' })}
          ${li({ ic: 'phone', t: '手机数据', s: '通讯录、日历、照片、位置…', tail: chev })}
          ${li({ ic: 'tune', t: '风格', s: '原生（Material You）', tail: chev })}
        </div>
        <div class="sec">账号</div>
        <div class="card list">
          ${li({ ic: 'mail', t: '邮箱', s: '2 个账号，存在 hub 上', tail: chev })}
          ${li({ ic: 'shield', t: '设备', s: '这台手机、1 台 iPhone', tail: chev })}
          ${li({ ic: 'info', t: '关于', s: '体验模式、隐私、开源许可', tail: chev })}
        </div>
      </div>
      ${navbar('settings')}`,
  });

  const sw = (on = true) => `<span class="switch ${on ? '' : 'off'}" data-switch></span>`;
  add({
    id: 'settings-notify', group: '设置', title: '通知设置',
    notes: [
      '上面两项和 iOS 一样；下面对应系统的四个通知渠道，可以一键跳到系统设置（ux.md 3.8）。',
      '「锁屏和状态栏」一组代替 iOS 的实时活动设置：常驻通知、状态栏胶囊、锁屏显示详情。',
    ],
    render: () => `
      ${appbar({ back: 'settings', title: '通知' })}
      <div class="scroll">
        <div class="sec">什么时候通知</div>
        <div class="card list">
          ${li({ t: '需要你处理时', s: '授权、计划、提问、终端里的对话框', tail: sw() })}
          ${li({ t: 'Agent 做完时', s: '只通知长任务；在电脑上看过的不通知', tail: sw() })}
        </div>
        <div class="sec">系统通知渠道<span class="grow"></span><button class="act">${icon('open_new', 'xs')}系统设置</button></div>
        <div class="card list">
          ${li({ ic: 'error', cls: 'bad', t: '需要你', s: '重要 · 横幅和声音', tail: chev })}
          ${li({ ic: 'check_circle', cls: 'ok', t: '做完了', s: '默认', tail: chev })}
          ${li({ ic: 'auto_awesome', t: 'Persona', s: '默认 · 助手主动找你', tail: chev })}
          ${li({ ic: 'wifi', t: '实时状态', s: '低 · 常驻的那一条，可以调成静默', tail: chev })}
        </div>
        <div class="sec">锁屏和状态栏</div>
        <div class="card list">
          ${li({ t: '锁屏常驻通知', s: '需要你 / 工作中 / 完成未看 三个数', tail: sw() })}
          ${li({ t: '状态栏胶囊', s: '显示「需要你」的数量', tail: chev })}
          ${li({ t: '锁屏显示详情', s: '关：锁屏只显示数量', tail: sw(false) })}
        </div>
      </div>`,
  });

  add({
    id: 'diag', group: '设置', title: '连接诊断',
    notes: [
      '当前线路一眼看清：公网 IPv6 / 域名 / 内网，TLS 加证书钉扎。',
      '候选地址全列出来，没试的写原因；「复制诊断信息」不含 token。',
      '心跳前台、后台分开：手机深睡时自己的计时会停，后台心跳由 hub 发，间隔连上时协商（流程图 6）。',
      '前缀变了以后人在外面只能靠域名找回，所以推荐给 hub 配 DDNS（流程图 7）。',
      '「后台断开」来自 hub 记下的连接时段（hub-changes.md 3.10）：国产系统查不到自启动这些设置，设没设对只能看结果。',
      '页面上的延迟是示例，不是实测；心跳间隔是设计值，要真机实测耗电后再定。',
    ],
    render: () => `
      ${appbar({ back: 'settings', title: '连接诊断' })}
      <div class="scroll">
        <div class="route">
          <div class="rt">${icon('shield')}<span>公网 IPv6 · TLS · 证书已钉扎</span></div>
          <div class="rs">10:24 起一直连着，应用层心跳正常；在这个 Wi-Fi 上记住了这条线路</div>
        </div>
        <div class="sec">hub 的地址 · 3</div>
        <div class="card list">
          <div class="ep"><span class="lic ok">${icon('public')}</span><div class="eb"><div class="et">公网 IPv6<span class="tag unread">当前</span></div><div class="ea mono">https://[2001:db8:12::7]:18662</div><div class="er">家宽重新拨号会换前缀，换了靠域名找回</div></div><span class="lat ok">23 ms</span></div>
          <div class="ep"><span class="lic">${icon('link')}</span><div class="eb"><div class="et">域名</div><div class="ea mono">https://hub.example.com:18662</div><div class="er">解析到 AAAA 和 A 记录</div></div><span class="lat ok">41 ms</span></div>
          <div class="ep"><span class="lic">${icon('router')}</span><div class="eb"><div class="et">内网</div><div class="ea mono">https://192.0.2.10:18662</div><div class="er">不在同一网络，没试</div></div><span class="lat na">—</span></div>
        </div>
        <div class="sec">这台手机</div>
        <div class="card list">
          ${li({ ic: 'wifi', t: 'Wi-Fi · 有公网 IPv6', s: '运营商分配的前缀，地址已隐藏' })}
          ${li({ ic: 'shield', t: '证书指纹', s: 'SHA-256 和配对时一致' })}
          ${li({ ic: 'schedule', t: '心跳', s: '前台 20 秒；后台约 4 分钟，由 hub 发（连上时协商）' })}
          ${li({ ic: 'history', t: '后台断开', s: '过去 24 小时 2 次，最长 3 分钟（hub 记录）' })}
        </div>
      </div>
      <div class="actions inline"><button class="btn outlined" data-toast="已复制（不含 token）">${icon('copy')}复制诊断信息</button><button class="btn filled">${icon('refresh')}重新探测</button></div>`,
  });


  /* ---------- routing ---------- */

  const $ = id => document.getElementById(id);
  const el = { device: $('device'), screen: $('screen'), backdrop: $('backdrop'), statusbar: $('statusbar'), content: $('content'), layer: $('layer'), dir: $('dir'), notes: $('notes') };
  const GROUPS = ['接入', 'Agents', '对话', '交互', '通知', 'Persona', '收件箱', '设置'];

  const readHash = () => {
    const q = new URLSearchParams(location.hash.slice(1));
    return {
      screen: SCREENS.some(s => s.id === q.get('screen')) ? q.get('screen') : 'agents',
      theme: q.get('theme') === 'dark' ? 'dark' : 'light',
      w: q.get('w') === '320' ? 320 : 360,
      only: q.get('only') || '',
      audit: q.has('audit'),
    };
  };
  const writeHash = patch => {
    const s = { ...readHash(), ...patch };
    const q = new URLSearchParams({ screen: s.screen, theme: s.theme });
    if (s.w === 320) q.set('w', '320');
    if (s.only) q.set('only', s.only);
    location.hash = q.toString();
  };

  const statusbarHtml = scr => `
    <span class="sb-left">10:24${scr.chip ? `<span class="live-chip"><span class="dot-w"></span>${scr.chip}</span>` : ''}</span>
    <span class="icons">${icon('wifi')}${icon('signal')}<span class="battery"></span></span>`;

  let toastTimer = 0;
  const toast = text => {
    el.layer.querySelector('.snackbar')?.remove();
    el.layer.insertAdjacentHTML('beforeend', `<div class="snackbar">${icon('check', 's')}<span>${text}</span></div>`);
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => el.layer.querySelector('.snackbar')?.remove(), 2200);
  };

  const render = () => {
    const st = readHash();
    const scr = SCREENS.find(s => s.id === st.screen);
    document.documentElement.dataset.theme = st.theme;
    document.body.classList.toggle('phone-only', st.only === 'phone');
    el.device.classList.toggle('narrow', st.w === 320);
    el.screen.className = `screen${scr.chrome === 'dark' ? ' chrome-dark' : ''}${scr.nav ? ' has-nav' : ''}`;
    el.backdrop.innerHTML = scr.backdrop ? scr.backdrop() : '';
    el.statusbar.innerHTML = statusbarHtml(scr);
    el.content.innerHTML = scr.render();
    el.layer.innerHTML = scr.layer ? scr.layer() : '';
    el.screen.querySelectorAll('.scroll').forEach(s => {
      if (s.hasAttribute('data-end')) s.scrollTop = s.scrollHeight;
      const f = s.querySelector('[data-focus]');
      if (f) s.scrollTop = f.offsetTop - s.offsetTop - (f.dataset.focus === 'top' ? 0 : s.clientHeight / 2 - f.offsetHeight / 2);
    });
    el.screen.querySelectorAll('textarea').forEach(syncField);
    document.title = `${scr.title} · Persona Android 预览`;

    el.dir.innerHTML = GROUPS.map(g => `<h3>${g}</h3><ul>${SCREENS.filter(s => s.group === g).map(s =>
      `<li><button class="${s.id === scr.id ? 'on' : ''}" data-screen="${s.id}">${s.title}</button></li>`).join('')}</ul>`).join('');
    el.notes.innerHTML = `<h4>这一页看什么 · ${scr.title}</h4><ul>${(scr.notes || []).map(n => `<li>${n}</li>`).join('')}</ul>`;
    document.querySelectorAll('[data-theme-set]').forEach(b => b.classList.toggle('on', b.dataset.themeSet === st.theme));
    document.querySelectorAll('[data-w-set]').forEach(b => b.classList.toggle('on', Number(b.dataset.wSet) === st.w));
    document.getElementById('audit')?.remove();
    if (st.audit) document.body.insertAdjacentHTML('beforeend', `<pre id="audit">AUDIT ${scr.id} ${st.w} ${JSON.stringify(audit())}</pre>`);
  };

  // #…&audit=1 lists buttons, chips and tags that are clipped or stick out of the screen (checked with headless Chrome).
  const audit = () => {
    const box = el.screen.getBoundingClientRect();
    const bad = [];
    el.screen.querySelectorAll('.btn, button, .chip, .tag, .roundbtn').forEach(b => {
      if (!b.getClientRects().length || b.closest('.chips')) return;
      const r = b.getBoundingClientRect();
      const name = b.textContent.trim().slice(0, 16) || b.getAttribute('aria-label');
      const range = document.createRange();
      range.selectNodeContents(b);
      const t = range.getBoundingClientRect();
      if (t.width && (t.left < r.left - 0.5 || t.right > r.right + 0.5)) bad.push(`clipped: ${name}`);
      const walker = document.createTreeWalker(b, NodeFilter.SHOW_TEXT);
      for (let n = walker.nextNode(); n; n = walker.nextNode()) {
        if (!n.textContent.trim()) continue;
        range.selectNodeContents(n);
        const tops = [...range.getClientRects()].map(x => x.top);
        if (tops.length > 1 && Math.max(...tops) - Math.min(...tops) > 4) bad.push(`wrapped: ${name}`);
      }
      if (r.right > box.right + 0.5 || r.left < box.left - 0.5) bad.push(`outside: ${name}`);
      if (r.height > 48.5) bad.push(`tall: ${name}`);
    });
    return bad;
  };

  // A reason or feedback in the box disables the buttons that would contradict it (ux.md 3.4).
  const syncField = ta => {
    ta.style.height = 'auto';
    ta.style.height = `${ta.scrollHeight + 2}px`;
    const box = ta.closest('.ixpanel, .sheet');
    if (!box) return;
    const filled = ta.value.trim() !== '';
    if (ta.hasAttribute('data-reason')) box.classList.toggle('has-reason', filled);
    if (ta.hasAttribute('data-feedback')) box.classList.toggle('has-feedback', filled);
  };

  document.addEventListener('input', e => { if (e.target.matches('textarea')) syncField(e.target); });
  document.addEventListener('click', e => {
    const t = e.target;
    const side = t.closest('[data-screen], [data-theme-set], [data-w-set]');
    if (side) {
      if (side.dataset.screen) writeHash({ screen: side.dataset.screen });
      if (side.dataset.themeSet) writeHash({ theme: side.dataset.themeSet });
      if (side.dataset.wSet) writeHash({ w: Number(side.dataset.wSet) });
      return;
    }
    const msg = t.closest('[data-toast]');
    if (msg) { e.stopPropagation(); toast(msg.dataset.toast); return; }
    const chip = t.closest('[data-chip]');
    if (chip) { chip.parentElement.querySelectorAll('[data-chip]').forEach(c => c.classList.toggle('on', c === chip)); return; }
    const opt = t.closest('[data-opt]');
    if (opt) { opt.parentElement.querySelectorAll('[data-opt]').forEach(o => o.classList.toggle('sel', o === opt)); return; }
    const sw = t.closest('[data-switch]');
    if (sw) { sw.classList.toggle('off'); return; }
    if (t.closest('textarea, input')) return;
    const go = t.closest('[data-go]');
    if (go) writeHash({ screen: go.dataset.go });
  });
  document.addEventListener('keydown', e => {
    if (e.target.matches('textarea, input') || (e.key !== 'ArrowLeft' && e.key !== 'ArrowRight')) return;
    const i = SCREENS.findIndex(s => s.id === readHash().screen);
    const next = SCREENS[(i + (e.key === 'ArrowRight' ? 1 : -1) + SCREENS.length) % SCREENS.length];
    writeHash({ screen: next.id });
  });
  window.addEventListener('hashchange', render);
  render();
})();
