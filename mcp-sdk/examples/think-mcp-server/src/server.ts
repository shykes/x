import { FastMCP, UserError } from "fastmcp";
import { z } from "zod";

// Create a new MCP server
const server = new FastMCP({
  name: "Think Tool Server",
  version: "1.0.0",
});

// Add the "think" tool
server.addTool({
  name: "think",
  description: "Use the tool to think about something. It will not obtain new information or change the database, but just append the thought to the log. Use it when complex reasoning or some cache memory is needed.",
  parameters: z.object({
    thought: z.string().describe("A thought to think about.")
  }),
  execute: async (args, { log }) => {
    // Log the thought (this will be visible in the server logs but not to the user)
    log.info("Thinking process", { thought: args.thought });
    
    // Simply return the thought itself, as per Anthropic's blog post
    return args.thought;
  },
});

// Start the server with stdio transport
server.start({
  transportType: "stdio",
});

// Use console.error instead of console.log - this writes to stderr which won't interfere with the protocol
console.error("Think Tool Server is running...");