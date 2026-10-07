import { useContext, useState } from "react";
import { Node, mergeAttributes } from "@tiptap/core";
import { NodeViewWrapper, ReactNodeViewRenderer, type NodeViewProps } from "@tiptap/react";
import { Button } from "@/components/ui";
import { Icon } from "@/components/icons";
import { BlogPostsDialog } from "@/features/blog/BlogPostsDialog";
import { LatestPosts } from "@/features/blog/LatestPosts";
import { BLOG_POSTS_NODE, blogPostsSettings, type BlogPostsSettings } from "@/features/blog/blogPosts";
import { t } from "@/i18n";
import { DocPageContext } from "./BlockViews";
import { ownEvent, settingsAttrs } from "./pageLists";

declare module "@tiptap/core" {
  interface Commands<ReturnType> {
    blogPosts: {
      /** Opens the settings dialog for a new latest blog posts block; the dialog inserts it. */
      pickBlogPosts: () => ReturnType;
      insertBlogPosts: (settings: BlogPostsSettings) => ReturnType;
    };
  }
}

export interface BlogPostsOptions {
  /** Opens the settings dialog for a new block; without it the slash menu's entry does nothing. */
  pick: (() => void) | undefined;
}

function BlogPostsView({ node, updateAttributes }: NodeViewProps) {
  const [editing, setEditing] = useState(false);
  const page = useContext(DocPageContext);
  const settings = blogPostsSettings(node.attrs);
  return (
    <NodeViewWrapper contentEditable={false} data-blog-posts-node="">
      <div className="doc-block">
        <div className="doc-block-settings" data-block-settings>
          <span className="min-w-0 flex-1 truncate text-xs">{t.blogPosts.title(settings.space)}</span>
          <Button size="sm" variant="secondary" icon={<Icon.Settings />} onClick={() => setEditing(true)} data-action="edit-blog-posts">
            {t.blogPosts.edit}
          </Button>
        </div>
        <LatestPosts settings={settings} inEditor />
      </div>
      {editing && (
        <BlogPostsDialog
          initial={settings}
          currentSpace={page?.spaceKey}
          isNew={false}
          onClose={() => setEditing(false)}
          onSave={(next) => {
            setEditing(false);
            updateAttributes(next);
          }}
        />
      )}
    </NodeViewWrapper>
  );
}

/** The newest blog posts of a space or of every space; the page stores which, each reader's view asks for the posts. */
export const BlogPostsNode = Node.create<BlogPostsOptions>({
  name: BLOG_POSTS_NODE,
  group: "block",
  atom: true,
  selectable: true,
  draggable: true,
  addOptions() {
    return { pick: undefined };
  },
  addAttributes() {
    return settingsAttrs(blogPostsSettings);
  },
  parseHTML() {
    return [{ tag: "div[data-blog-posts]" }];
  },
  renderHTML({ HTMLAttributes }) {
    return ["div", mergeAttributes(HTMLAttributes, { "data-blog-posts": "" })];
  },
  // Its posts are not the page's words, so copied text leaves it out.
  renderText() {
    return "";
  },
  addNodeView() {
    return ReactNodeViewRenderer(BlogPostsView, { stopEvent: ownEvent });
  },
  addCommands() {
    return {
      pickBlogPosts: () => () => {
        this.options.pick?.();
        return true;
      },
      insertBlogPosts:
        (settings) =>
        ({ commands }) =>
          commands.insertContent({ type: this.name, attrs: blogPostsSettings({ ...settings }) }),
    };
  },
});
