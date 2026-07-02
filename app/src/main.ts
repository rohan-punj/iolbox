import { mount } from "svelte";
import "./styles/theme.css";
// Import for its side effect: constructs the ThemeStore singleton, which writes
// data-theme onto <html> before the app mounts (prevents an unthemed flash).
import "./lib/themeStore.svelte";
import App from "./App.svelte";

const app = mount(App, {
  target: document.getElementById("app")!,
});

export default app;
