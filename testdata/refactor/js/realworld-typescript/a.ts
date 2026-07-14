interface Widget {
  label: string;
  size: number;
}

type WidgetPair = { first: Widget; second: Widget };

function makeWidgetA(spec: string, size: number = 1): Widget {
  const parts = spec.split(":");
  const label = parts[0] + "/v1";
  const widget = { label: label, size: size };
  return widget;
}

const buildLabelA = (name: string): string => {
  const trimmed = name.trim();
  const prefix = "user:";
  const label = prefix + trimmed;
  return label;
};

function pickFirstA<T extends Widget>(items: T[], fallback: T): T {
  if (items.length === 0) {
    return fallback;
  }
  const found = items[0];
  return found;
}

class ItemStoreA {
  private async loadA(id: string): Promise<Widget> {
    const key = "store:v1:" + id;
    const raw = await this.backend.get(key);
    const widget = JSON.parse(raw);
    return widget;
  }
}
