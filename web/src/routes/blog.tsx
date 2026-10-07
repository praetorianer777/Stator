import { createRoute } from "@tanstack/react-router";
import { BlogScreen, type BlogFilter } from "@/features/blog/BlogScreen";
import { spaceRoute } from "./space";

/** A year, and a month only with one, kept when they are numbers a blog can show. */
export function blogSearch(search: Record<string, unknown>): BlogFilter {
  const year = Number(search.year);
  if (!Number.isInteger(year) || year < 1970 || year > 9999) return {};
  const month = Number(search.month);
  return Number.isInteger(month) && month >= 1 && month <= 12 ? { year, month } : { year };
}

/** A space's blog, filtered to a year or a month by its address, so a link to a month opens on it. */
export const spaceBlogRoute = createRoute({
  getParentRoute: () => spaceRoute,
  path: "/blog",
  validateSearch: blogSearch,
  component: function BlogRoute() {
    const { spaceKey } = spaceBlogRoute.useParams();
    const filter = spaceBlogRoute.useSearch();
    return <BlogScreen spaceKey={spaceKey} filter={filter} />;
  },
});
