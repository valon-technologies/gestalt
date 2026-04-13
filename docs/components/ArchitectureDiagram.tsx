'use client';

import { useRef, useState, useEffect } from 'react';
import ArchitectureLight from './ArchitectureLight';
import ArchitectureDark from './ArchitectureDark';

const NATIVE_WIDTH = 1350;
const NATIVE_HEIGHT = 1100;
const CROP_TOP = 310;
const CROP_BOTTOM = 310;
const VISIBLE_HEIGHT = NATIVE_HEIGHT - CROP_TOP - CROP_BOTTOM;

export default function ArchitectureDiagram() {
  const ref = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(0);

  useEffect(() => {
    if (!ref.current) return;
    const update = () => {
      if (ref.current) setScale(ref.current.clientWidth / NATIVE_WIDTH);
    };
    const observer = new ResizeObserver(update);
    observer.observe(ref.current);
    update();
    return () => observer.disconnect();
  }, []);

  return (
    <div ref={ref} style={{ width: '100%', overflow: 'hidden', marginTop: '2rem', marginBottom: '1rem' }}>
      <div
        style={{
          transformOrigin: 'top left',
          transform: `scale(${scale}) translateY(-${CROP_TOP}px)`,
          height: scale ? `${VISIBLE_HEIGHT * scale}px` : undefined,
          opacity: scale ? 1 : 0,
        }}
      >
        <div className="arch-light">
          <ArchitectureLight />
        </div>
        <div className="arch-dark">
          <ArchitectureDark />
        </div>
      </div>
    </div>
  );
}
