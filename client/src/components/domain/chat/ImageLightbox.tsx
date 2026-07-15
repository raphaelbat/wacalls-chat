import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import {
  Download,
  Maximize2,
  Minimize2,
  RotateCcw,
  RotateCw,
  X,
  ZoomIn,
  ZoomOut,
} from "lucide-react";

interface Props {
  src: string;
  alt?: string;
  onClose: () => void;
}

// ImageLightbox renders an in-chat fullscreen overlay (no new tab) with
// zoom, rotate and download controls. Mounted via a portal so it sits
// above the chat layout but stays inside the SPA.
export const ImageLightbox = ({ src, alt, onClose }: Props) => {
  const [scale, setScale] = useState(1);
  const [rotation, setRotation] = useState(0);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      if (e.key === "+" || e.key === "=") setScale((s) => Math.min(6, s + 0.25));
      if (e.key === "-") setScale((s) => Math.max(0.25, s - 0.25));
    };
    window.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      window.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onClose]);

  const handleDownload = () => {
    const a = document.createElement("a");
    a.href = src;
    a.download = alt || "imagem";
    a.rel = "noopener";
    document.body.appendChild(a);
    a.click();
    a.remove();
  };

  return createPortal(
    <div
      className="fixed inset-0 z-[100] flex flex-col bg-black/90 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        className="flex items-center justify-end gap-1 border-b border-white/10 px-3 py-2 text-white"
        onClick={(e) => e.stopPropagation()}
      >
        <ToolBtn label="Diminuir" onClick={() => setScale((s) => Math.max(0.25, s - 0.25))}>
          <ZoomOut className="h-4 w-4" />
        </ToolBtn>
        <span className="min-w-[3rem] text-center text-xs tabular-nums opacity-80">
          {Math.round(scale * 100)}%
        </span>
        <ToolBtn label="Aumentar" onClick={() => setScale((s) => Math.min(6, s + 0.25))}>
          <ZoomIn className="h-4 w-4" />
        </ToolBtn>
        <ToolBtn
          label="Tamanho original"
          onClick={() => {
            setScale(1);
            setRotation(0);
          }}
        >
          <Minimize2 className="h-4 w-4" />
        </ToolBtn>
        <ToolBtn label="Ajustar" onClick={() => setScale(2)}>
          <Maximize2 className="h-4 w-4" />
        </ToolBtn>
        <span className="mx-2 h-5 w-px bg-white/20" />
        <ToolBtn label="Girar -90°" onClick={() => setRotation((r) => r - 90)}>
          <RotateCcw className="h-4 w-4" />
        </ToolBtn>
        <ToolBtn label="Girar +90°" onClick={() => setRotation((r) => r + 90)}>
          <RotateCw className="h-4 w-4" />
        </ToolBtn>
        <span className="mx-2 h-5 w-px bg-white/20" />
        <ToolBtn label="Baixar" onClick={handleDownload}>
          <Download className="h-4 w-4" />
        </ToolBtn>
        <ToolBtn label="Fechar" onClick={onClose}>
          <X className="h-4 w-4" />
        </ToolBtn>
      </div>
      <div
        className="flex flex-1 items-center justify-center overflow-auto p-4"
        onClick={onClose}
      >
        <img
          src={src}
          alt={alt || ""}
          draggable={false}
          onClick={(e) => e.stopPropagation()}
          style={{
            transform: `scale(${scale}) rotate(${rotation}deg)`,
            transition: "transform 0.15s ease-out",
          }}
          className="max-h-full max-w-full select-none object-contain shadow-2xl"
        />
      </div>
    </div>,
    document.body,
  );
};

const ToolBtn = ({
  children,
  label,
  onClick,
}: {
  children: React.ReactNode;
  label: string;
  onClick: () => void;
}) => (
  <button
    type="button"
    title={label}
    aria-label={label}
    onClick={onClick}
    className="grid h-8 w-8 place-items-center rounded-md text-white/90 transition hover:bg-white/10 hover:text-white"
  >
    {children}
  </button>
);