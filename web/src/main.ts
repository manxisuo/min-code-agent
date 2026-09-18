import { createApp } from "vue";
import App from "./App.vue";
import "./style.css";
import { useTheme } from "./theme";

// Restore saved / system theme before first paint styles matter.
useTheme();

createApp(App).mount("#app");
