export default function Loading() {
  return (
    <div className="mx-auto grid max-w-[1700px] gap-6 lg:grid-cols-[minmax(0,1fr)_380px]" aria-busy="true" aria-label="Loading video">
      <div className="space-y-4">
        <div className="skeleton aspect-video rounded-card" />
        <div className="skeleton h-6 w-3/4 rounded" />
        <div className="skeleton h-10 w-56 rounded-full" />
        <div className="flex items-center gap-3"><div className="skeleton size-11 rounded-full" /><div className="skeleton h-4 w-40 rounded" /></div>
        <div className="skeleton h-24 rounded-card" />
      </div>
      <div className="space-y-3">
        {Array.from({ length: 6 }, (_, i) => (
          <div key={i} className="flex gap-3"><div className="skeleton aspect-video w-40 shrink-0 rounded-xl" /><div className="flex-1 space-y-2"><div className="skeleton h-3.5 w-11/12 rounded" /><div className="skeleton h-3 w-1/2 rounded" /></div></div>
        ))}
      </div>
    </div>
  );
}
