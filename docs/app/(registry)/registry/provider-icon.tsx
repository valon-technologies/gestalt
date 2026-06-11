"use client";

import { useEffect, useState } from "react";
import {
  type Provider,
  isSvgIconUrl,
  loadSvgIconDataUrl,
} from "./registry-data";
import styles from "./registry.module.css";

export default function ProviderIcon({
  provider,
  large = false,
}: {
  provider: Provider;
  large?: boolean;
}) {
  const renderableIconUrl = useRenderableIconUrl(provider.iconUrl);

  if (renderableIconUrl) {
    return (
      // eslint-disable-next-line @next/next/no-img-element
      <img
        alt=""
        className={large ? styles.largeIcon : styles.icon}
        src={renderableIconUrl}
      />
    );
  }
  return (
    <span className={large ? styles.largeFallbackIcon : styles.fallbackIcon}>
      {provider.displayName.slice(0, 1).toUpperCase()}
    </span>
  );
}

function useRenderableIconUrl(iconUrl: string | null) {
  const [renderableIconUrl, setRenderableIconUrl] = useState<string | null>(
    iconUrl && !isSvgIconUrl(iconUrl) ? iconUrl : null,
  );

  useEffect(() => {
    let cancelled = false;
    if (!iconUrl) {
      setRenderableIconUrl(null);
      return () => {
        cancelled = true;
      };
    }
    if (!isSvgIconUrl(iconUrl)) {
      setRenderableIconUrl(iconUrl);
      return () => {
        cancelled = true;
      };
    }
    setRenderableIconUrl(null);
    void loadSvgIconDataUrl(iconUrl).then((dataUrl) => {
      if (!cancelled) {
        setRenderableIconUrl(dataUrl);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [iconUrl]);

  return renderableIconUrl;
}
