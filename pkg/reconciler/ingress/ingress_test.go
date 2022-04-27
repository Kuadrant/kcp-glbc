package ingress

import "testing"

func Test_awsEndpointWeight(t *testing.T) {
	type args struct {
		numIPs int
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "single ip",
			args: args{
				numIPs: 1,
			},
			want: "120",
		},
		{
			name: "multiple ips 2",
			args: args{
				numIPs: 2,
			},
			want: "60",
		},
		{
			name: "multiple ips 3",
			args: args{
				numIPs: 3,
			},
			want: "40",
		},
		{
			name: "multiple ips 4",
			args: args{
				numIPs: 4,
			},
			want: "30",
		},
		{
			name: "60 ips",
			args: args{
				numIPs: 60,
			},
			want: "2",
		},
		{
			name: "61 ips",
			args: args{
				numIPs: 61,
			},
			want: "1",
		},
		{
			name: "ips equal to max weight (120)",
			args: args{
				numIPs: 120,
			},
			want: "1",
		},
		{
			name: "more IPs than max weight (121)",
			args: args{
				numIPs: 121,
			},
			want: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := awsEndpointWeight(tt.args.numIPs); got != tt.want {
				t.Errorf("awsEndpointWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}
