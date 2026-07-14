interface Widget {
  label: string;
  size: number;
}

type WidgetPair = { first: Widget; second: Widget };

function makeWidgetB(spec: string, size: number = 1): Widget {
  const parts = spec.split(":");
  const label = parts[0] + "/v2";
  const widget = { label: label, size: size };
  return widget;
}

const buildLabelB = (name: string): string => {
  const trimmed = name.trim();
  const prefix = "admin:";
  const label = prefix + trimmed;
  return label;
};

function pickFirstB<T extends Widget>(items: T[], fallback: T): T {
  if (items.length === 0) {
    return fallback;
  }
  const found = items[items.length - 1];
  return found;
}

class OrderStoreB {
  private async loadB(id: string): Promise<Widget> {
    const key = "store:v2:" + id;
    const raw = await this.backend.get(key);
    const widget = JSON.parse(raw);
    return widget;
  }
}
