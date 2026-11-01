import {
  House, Compass, TrendUp, SquaresFour, ClockCounterClockwise, Clock, Heart, ListPlus,
  UploadSimple, ChartBar, Gear, User, Plus, FilmStrip, Broadcast, type Icon,
} from "@phosphor-icons/react";

const map: Record<string, Icon> = {
  House, Compass, TrendUp, SquaresFour, ClockCounterClockwise, Clock, Heart, ListPlus,
  UploadSimple, ChartBar, Gear, User, Plus, FilmStrip, Broadcast,
};

export function NavIcon({ name, size = 20, weight = "regular" }: { name: string; size?: number; weight?: "regular" | "fill" }) {
  const C = map[name] ?? House;
  return <C size={size} weight={weight} aria-hidden="true" />;
}
