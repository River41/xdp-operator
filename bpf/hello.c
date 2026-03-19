#include <linux/bpf.h>
#include <bpf/bpf_helpers.h>

SEC("xdp")
int hello_xdp(struct xdp_md *ctx) {
    char fmt[] = "XDP program triggered!\n";
    bpf_trace_printk(fmt, sizeof(fmt));
    return XDP_PASS;
}

char _license[] SEC("license") = "GPL";