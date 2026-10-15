from pathlib import Path

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parents[1]
DEFAULT_PUBLIC = ROOT / "web" / "default" / "public"


def make_logo(size: int) -> Image.Image:
    image = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(image)

    bg = (248, 251, 255, 255)
    border = (219, 234, 254, 255)
    grad_start = (56, 189, 248, 255)
    grad_end = (37, 99, 235, 255)
    slash = (15, 23, 42, 255)
    glow = (56, 189, 248, 46)

    radius = max(14, int(size * 0.26))
    inset = max(3, int(size * 0.045))
    draw.rounded_rectangle(
        (inset, inset, size - inset, size - inset),
        radius=radius,
        fill=bg,
        outline=border,
        width=max(2, int(size * 0.018)),
    )

    def lerp(a: int, b: int, t: float) -> int:
        return int(a + (b - a) * t)

    def gradient_color(t: float) -> tuple[int, int, int, int]:
        return tuple(lerp(grad_start[i], grad_end[i], t) for i in range(4))

    def poly(points: list[tuple[float, float]], color: tuple[int, int, int, int]) -> None:
        scaled = [(x * size, y * size) for x, y in points]
        draw.polygon(scaled, fill=color)

    left_points = [
        (0.34, 0.22),
        (0.18, 0.50),
        (0.34, 0.78),
        (0.40, 0.73),
        (0.28, 0.50),
        (0.40, 0.27),
    ]
    right_points = [
        (0.66, 0.22),
        (0.60, 0.27),
        (0.72, 0.50),
        (0.60, 0.73),
        (0.66, 0.78),
        (0.82, 0.50),
    ]
    poly(left_points, gradient_color(0.18))
    poly(right_points, gradient_color(0.82))

    slash_width = max(4, int(size * 0.072))
    draw.line(
        ((0.545 * size, 0.23 * size), (0.435 * size, 0.79 * size)),
        fill=slash,
        width=slash_width,
        joint="curve",
    )

    glow_radius = max(3, int(size * 0.06))
    cx, cy = size * 0.5, size * 0.5
    draw.ellipse(
        (cx - glow_radius, cy - glow_radius, cx + glow_radius, cy + glow_radius),
        fill=glow,
    )

    return image


def write_targets(public_dir: Path) -> None:
    public_dir.mkdir(parents=True, exist_ok=True)
    logo = make_logo(512)
    logo.save(public_dir / "logo.png")

    favicon = make_logo(64)
    favicon.save(
        public_dir / "favicon.ico",
        format="ICO",
        sizes=[(16, 16), (32, 32), (48, 48), (64, 64)],
    )


def main() -> None:
    write_targets(DEFAULT_PUBLIC)


if __name__ == "__main__":
    main()
