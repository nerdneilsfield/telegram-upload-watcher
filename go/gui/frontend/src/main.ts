import './app.css';
import { provideFluentDesignSystem, allComponents } from '@fluentui/web-components';
import App from './App.svelte';

provideFluentDesignSystem().register(allComponents);

const app = new App({
  target: document.getElementById('app')!
});

export default app;
