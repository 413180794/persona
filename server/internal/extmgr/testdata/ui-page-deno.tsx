/** @jsxImportSource ../../../../sdk/ts/src */
import { componentPage } from "../../../../sdk/ts/src/index.ts";
import { View, Section, Text, Button, callback } from "../../../../sdk/ts/src/components.ts";
import { useState, useInterval } from "../../../../sdk/ts/src/hooks.ts";

// Separately bundled default export: no transport/Node injection into the guest.
let writes = 0, uiTicks = 0, serviceTicks = 0, lastValues: unknown;
setInterval(() => serviceTicks++, 50);
function Home() {
  const [count, setCount] = useState(0);
  useInterval(() => { uiTicks++; }, 50);
  return <View key="root" title="Synthetic fixture" review={{count: 0, name: "summary", buttons: [{
    title: "Review", action: callback(values => {writes++; lastValues = values; return {toast: "Review submitted"};}, {submit: true, values: {saved: "native"}}),
  }]}}><Section key="main">
    <Text key="value" text={String(count)} />
    <Text key="padding" text={"Synthetic fixture ".repeat(50)} />
    <Button key="toast" title="Toast" action={callback(() => {writes++; return {toast: "Saved"};})} />
    <Button key="push" title="Push" action={callback(() => ({push: {ui: "detail", state: "fixture", title: "Detail"}}))} />
    <Button key="pop" title="Pop" action={callback(() => ({pop: true}))} />
    <Button key="open" title="Open" action={callback(() => ({open_url: "https://example.com/fixture"}))} />
    <Button key="lines" title="Lines" action={callback(() => ({lines: {path: "fixture.go", start: 1, lines: [], total: 0}, pop: false}))} />
    <Button key="increment" title="Increment" action={callback(() => setCount(count + 1))} />
    <Button key="bad" title="Unsafe" action={callback(() => ({open_url: "javascript:alert(1)"}))} />
  </Section></View>;
}
export default {
  id: "example-page", version: "1.0.0",
  ui: {
    home: componentPage(() => <Home key="page" />),
    detail: componentPage(() => <View key="detail"><Section key="main"><Text key="detail-text" text="Detail" /></Section></View>),
  },
  shortcuts: {stats: () => ({text: JSON.stringify({writes, uiTicks, serviceTicks, lastValues})})},
};
